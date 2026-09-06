package daemon

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// A supervised runner is one atrium owns: spawned under a pseudo terminal,
// with its output captured and its process id known.
//
// Owning the process is one change with four payoffs. Terminate works, because
// there is a real pid to signal. The liveness reaper works, because it has a
// pid to ask the operating system about. Attach becomes possible, because the
// output exists. And shutdown can wait for the runner, because atrium is its
// parent.
//
// The trade against window mode is real and runs the other way: a window mode
// runner outlives atrium entirely, while a supervised one dies with it. Neither
// is strictly better, which is why launch mode is a per harness setting rather
// than a migration.

// How much recent output is kept per runner is a SETTING, and it lives in
// `internal/api/scrollback.go` next to the browser's half of the same
// question. Read here at spawn, so a change applies to the next runner started.
//
// It is still not a transcript. The durable record of what happened is the
// event log, and writing every byte a runner emits to SQLite would grow
// without bound to buy very little.

// ringBuffer keeps the last N bytes written to it.
type ringBuffer struct {
	mu   sync.Mutex
	data []byte
	full bool
	at   int
}

func newRing(size int) *ringBuffer { return &ringBuffer{data: make([]byte, size)} }

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	// A write larger than the whole buffer keeps only its tail.
	if n >= len(r.data) {
		copy(r.data, p[n-len(r.data):])
		r.at, r.full = 0, true
		return n, nil
	}
	first := copy(r.data[r.at:], p)
	if first < n {
		copy(r.data, p[first:])
		r.full = true
	}
	r.at += n
	if r.at >= len(r.data) {
		r.at -= len(r.data)
		r.full = true
	}
	return n, nil
}

// Snapshot returns the retained output, oldest first.
func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.full {
		out := make([]byte, r.at)
		copy(out, r.data[:r.at])
		return out
	}
	out := make([]byte, 0, len(r.data))
	out = append(out, r.data[r.at:]...)
	out = append(out, r.data[:r.at]...)
	return out
}

// runner is one live supervised process.
type runner struct {
	taskID string
	pty    pty.Pty
	cmd    *pty.Cmd
	// started separates "fell over on startup" from "finished". The first gets
	// its last output put on the card.
	started time.Time
	// resumed is set when this was launched with a harness's resume arguments.
	//
	// A resume id goes stale: the conversation is archived, or the runner's own
	// history is cleared, or the id belongs to a directory that has moved. The
	// runner then exits within a second saying so, which reaches the board as
	// a dead card and a terminal that never appeared. Knowing the launch asked
	// for a resume is what lets that be retried as a fresh start.
	resumed bool
	// spec is what to run to try again, without the resume. Empty when there
	// is nothing to fall back to.
	spec *launchSpec

	mu  sync.Mutex
	buf *ringBuffer
	// views is what size each attached viewer can draw. See `setViewport`:
	// the pty gets the smallest of them, because a shared terminal has one
	// size and several windows.
	views     map[any]viewport
	watchers  map[chan []byte]struct{}
	done      chan struct{}
	exitOnce  sync.Once
	closeOnce sync.Once
	// seen is when somebody was last attached, which only a shell reads.
	//
	// A runner is kept alive by being a runner: it holds the work, and closing
	// it because nobody was looking would throw away what it was doing. A
	// shell has no work, so the question "is anyone still interested in this"
	// is the only thing that decides whether it should still be running.
	seen time.Time
}

// closePTY closes the pseudo terminal, at most once.
//
// TWO PATHS REACH IT. `windDown` closes the terminal when a runner will not
// take the hint, and `awaitExit` closes it when `cmd.Wait` returns, which is
// what closing it caused.
//
// A double close of a Windows handle is not a Go panic. The process disappears
// with no output at all: no stack, no exit message, nothing in a log. That is
// why the guard is here rather than a check at either call site.
func (r *runner) closePTY() {
	r.closeOnce.Do(func() {
		if r.pty != nil {
			_ = r.pty.Close()
		}
	})
}

// touch marks this terminal as looked at just now.
func (r *runner) touch() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.seen = time.Now()
}

func (r *runner) lastSeen() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.seen
}

// watching says whether anybody is attached right now.
func (r *runner) watching() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.watchers) > 0
}

// Say types text into the runner and then presses Enter, as two separate
// writes.
//
// NOT ONE WRITE, and the difference is the whole function.
//
// A TUI decides whether input is TYPED or PASTED from how it arrives, and a
// burst is a paste. `text + "\r"` in a single write is one burst, so the
// trailing carriage return is read as part of the pasted text rather than as
// the key that submits it. The message lands in the prompt, a newline appears,
// and nothing is sent: the operator ends up switching to the terminal and
// pressing Enter themselves, which is the whole thing they were avoiding.
//
// The pause is what separates them. It is short enough not to be felt and long
// enough to end the burst.
//
// Every path that says something to a session goes through here: a message, a
// note being sent, and an action's prompt.
func (r *runner) Say(text string) error {
	if err := r.Write([]byte(text)); err != nil {
		return err
	}
	time.Sleep(sayThenEnter)
	return r.Write([]byte("\r"))
}

// How long between the text and the Enter that sends it.
const sayThenEnter = 140 * time.Millisecond

// Write sends keystrokes to the runner. Nothing arbitrates between two
// attachers typing at once, which is the same situation as two hands on one
// keyboard.
func (r *runner) Write(p []byte) error {
	_, err := r.pty.Write(p)
	return err
}

// A pseudo terminal has ONE size and a shared session has several viewers.
//
// This used to be a pass-through, so the last browser to resize won and every
// other viewer kept drawing at its own width. The result is not a cosmetic
// mismatch: the runner wraps its output for the size it was told, the wider
// viewer renders those already-wrapped lines against a wider grid, and the
// screen fills with torn text, duplicated status lines and rows that never
// clear. Dragging a shared window resized somebody else's terminal.
//
// THE SMALLEST VIEWER DECIDES, which is what every multiplexer settled on for
// the same reason. Every attached viewer can then render what it is sent
// correctly, and the cost is unused margin in the larger window rather than
// a screen nobody can read. First attach and last detach are both just
// recomputes.
type viewport struct{ cols, rows int }

// setViewport records one viewer's size and applies the agreed one.
//
// Keyed by the attachment rather than counted, because a viewer that goes away
// has to stop constraining the others: a phone that attached once and closed
// its tab would otherwise hold the session at eighty columns forever.
func (r *runner) setViewport(id any, cols, rows int) error {
	if cols <= 0 || rows <= 0 {
		return nil
	}
	r.mu.Lock()
	if r.views == nil {
		r.views = map[any]viewport{}
	}
	r.views[id] = viewport{cols, rows}
	agreed := smallestViewport(r.views)
	r.mu.Unlock()
	return r.pty.Resize(agreed.cols, agreed.rows)
}

// dropViewport forgets a viewer that has detached and gives the size back.
func (r *runner) dropViewport(id any) {
	r.mu.Lock()
	if r.views == nil {
		r.mu.Unlock()
		return
	}
	if _, had := r.views[id]; !had {
		r.mu.Unlock()
		return
	}
	delete(r.views, id)
	agreed := smallestViewport(r.views)
	left := len(r.views)
	r.mu.Unlock()
	// Nothing to grow back to when the last viewer leaves. Resizing to zero
	// would be a resize to nothing, and the size a detached session keeps is
	// the one it had, which is what a runner reading it expects.
	if left == 0 {
		return
	}
	_ = r.pty.Resize(agreed.cols, agreed.rows)
}

// smallestViewport is the largest size every viewer can draw.
func smallestViewport(all map[any]viewport) viewport {
	out := viewport{}
	for _, v := range all {
		if out.cols == 0 || v.cols < out.cols {
			out.cols = v.cols
		}
		if out.rows == 0 || v.rows < out.rows {
			out.rows = v.rows
		}
	}
	return out
}

// subscribe returns the retained output plus a channel of everything after it.
func (r *runner) subscribe() ([]byte, chan []byte) {
	ch := make(chan []byte, 64)
	r.mu.Lock()
	defer r.mu.Unlock()
	select {
	case <-r.done:
		close(ch)
		return r.buf.Snapshot(), ch
	default:
	}
	r.watchers[ch] = struct{}{}
	return r.buf.Snapshot(), ch
}

func (r *runner) unsubscribe(ch chan []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.watchers[ch]; ok {
		delete(r.watchers, ch)
		close(ch)
	}
}

// fanout hands a chunk to every attacher. A slow attacher is dropped rather
// than allowed to block, because a blocked reader would eventually stall the
// runner itself.
func (r *runner) fanout(chunk []byte) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.watchers {
		cp := make([]byte, len(chunk))
		copy(cp, chunk)
		select {
		case ch <- cp:
		default:
		}
	}
}

func (r *runner) closeWatchers() {
	r.mu.Lock()
	defer r.mu.Unlock()
	for ch := range r.watchers {
		close(ch)
		delete(r.watchers, ch)
	}
}

// supervisor holds every runner atrium owns.
//
// TWO MAPS, BOTH KEYED BY CARD, and keeping them apart is deliberate. `runners`
// is the process doing the work: the reaper asks the operating system about it,
// the park tells it to stop, shelving refuses on its behalf, and its exit files
// the card as dead. `shells` is the plain shell an operator may open beside a
// wedged agent, which is none of those things.
//
// Folding a shell into `runners` puts it in front of every one of those
// callers, which all say `get(taskID)` and mean the runner, and the first
// consequence is a card filed as dead when somebody closes a shell. See
// `shell.go`.
type supervisor struct {
	mu      sync.Mutex
	runners map[string]*runner
	shells  map[string]*runner
}

func newSupervisor() *supervisor {
	return &supervisor{runners: map[string]*runner{}, shells: map[string]*runner{}}
}

func (s *supervisor) get(taskID string) *runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runners[taskID]
}

// has is `get` for callers that only want the question answered, so they do
// not hold a runner they have no business writing to.
func (s *supervisor) has(taskID string) bool { return s.get(taskID) != nil }

func (s *supervisor) add(r *runner) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runners[r.taskID] = r
}

func (s *supervisor) remove(taskID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.runners, taskID)
}

func (s *supervisor) all() []*runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]*runner, 0, len(s.runners))
	for _, r := range s.runners {
		out = append(out, r)
	}
	return out
}

// launchSpec is enough to start the same runner again without its resume.
//
// Held so a stale resume id can be retried as a fresh start rather than
// reaching the board as a dead card. Only the fields spawnPTY needs.
type launchSpec struct {
	cmd  string
	args []string
	cwd  string
	env  []string
}

// spawnPTY starts a runner under a pseudo terminal and returns its pid.
func (d *Daemon) spawnPTY(taskID, cmdName string, args []string, cwd string, env []string) (int, error) {
	return d.spawnPTYResume(taskID, cmdName, args, cwd, env, false, nil)
}

// spawnPTYResume is spawnPTY, told whether this launch used a resume id and
// what to run instead if that turns out to be stale.
func (d *Daemon) spawnPTYResume(taskID, cmdName string, args []string, cwd string, env []string,
	resumed bool, fresh *launchSpec) (int, error) {
	// Resolve on PATH before setting a working directory. go-pty resolves the
	// command relative to Dir, so a bare `claude` or `cmd.exe` would be looked
	// for inside the repo being worked in and reported as missing.
	resolved, err := exec.LookPath(cmdName)
	if err != nil {
		return 0, fmt.Errorf("%s is not on PATH: %w", cmdName, err)
	}
	resolved, args = viaShellIfScript(resolved, args)

	p, err := pty.New()
	if err != nil {
		return 0, fmt.Errorf("could not open a pseudo terminal: %w", err)
	}
	c := p.Command(resolved, args...)
	c.Dir = cwd
	c.Env = env
	if err := c.Start(); err != nil {
		p.Close()
		return 0, fmt.Errorf("could not start %s: %w", cmdName, err)
	}

	r := &runner{
		taskID: taskID, pty: p, cmd: c, started: time.Now(),
		resumed:  resumed,
		spec:     fresh,
		buf:      newRing(api.ScrollbackBytes(d.st)),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	d.sup.add(r)

	// A card that was lent out gets its address back the moment it has a
	// terminal to serve. Cheap and silent for the overwhelming majority that
	// were never shared, which is why it can sit on the path every runner
	// takes rather than being remembered at each of the several places one
	// starts.
	d.EnsureCardShare(taskID)

	// One reader owns the pty. Everything else subscribes to it.
	go func() {
		chunk := make([]byte, 8192)
		for {
			n, err := p.Read(chunk)
			if n > 0 {
				_, _ = r.buf.Write(chunk[:n])
				r.fanout(chunk[:n])
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("[atrium] %s output ended: %v", taskID, err)
				}
				return
			}
		}
	}()

	go d.awaitExit(r)

	pid := 0
	if c.Process != nil {
		pid = c.Process.Pid
	}
	return pid, nil
}

// awaitExit records the runner exiting and marks its card dead.
func (d *Daemon) awaitExit(r *runner) {
	err := r.cmd.Wait()
	lived := time.Since(r.started)
	r.exitOnce.Do(func() { close(r.done) })
	r.closeWatchers()
	// Read before the terminal closes. For a runner that dies on startup this
	// text is the reason, and the only copy of it.
	tail := lastOutput(r.buf.Snapshot(), 12)
	r.closePTY()
	d.sup.remove(r.taskID)
	// The process is gone, so nothing it was doing is still true.
	d.act.forget(r.taskID)

	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
	}
	payload := map[string]any{
		"exit_code": code, "by": "supervisor",
		"ran_for": lived.Round(time.Millisecond).String(),
	}
	// A runner that lasted seconds did no work, so its last output is a failure
	// message. One that ran for an hour ended for reasons its final twelve
	// lines do not explain.
	if lived < startupFailureWindow && tail != "" {
		payload["output"] = tail
		if err := d.st.SetWhy(r.taskID, "failed to start: "+firstLine(tail)); err != nil {
			log.Printf("[atrium] note early exit for %s: %v", r.taskID, err)
		}
	}
	if err := d.st.AppendEvent(r.taskID, store.EventExited, payload); err != nil {
		log.Printf("[atrium] record exit for %s: %v", r.taskID, err)
	}

	// A resume that died on the way up gets one try as a fresh start.
	//
	// A resume id goes stale for ordinary reasons: the conversation was
	// archived, the runner's history was cleared, the directory moved. The
	// runner then exits in about a second saying so, and the operator asked
	// for a terminal and got a dead card. Starting fresh is what they meant:
	// resume this work IF YOU CAN.
	//
	// Bounded to one attempt, and only when the failure looks like startup:
	// a runner that ran for an hour and exited non-zero is a session that
	// ended, and restarting that would be a loop that reopens a terminal
	// somebody deliberately closed.
	if r.resumed && r.spec != nil && code != 0 && lived < startupFailureWindow {
		log.Printf("[atrium] %s could not resume, starting it fresh: %s",
			r.taskID, firstLine(tail))
		if err := d.st.AppendEvent(r.taskID, store.EventLaunched, map[string]any{
			"by": "supervisor", "retried": "without the stored resume id",
			"because": firstLine(tail),
		}); err != nil {
			log.Printf("[atrium] record retry for %s: %v", r.taskID, err)
		}
		// The stored id is known bad. Left in place it would be tried again on
		// the next start, which is the same failure tomorrow.
		if err := d.st.SetResumeID(r.taskID, ""); err != nil {
			log.Printf("[atrium] clear stale resume for %s: %v", r.taskID, err)
		}
		if err := d.st.SetWhy(r.taskID, ""); err != nil {
			log.Printf("[atrium] clear why for %s: %v", r.taskID, err)
		}
		if _, err := d.spawnPTY(r.taskID, r.spec.cmd, r.spec.args, r.spec.cwd, r.spec.env); err != nil {
			log.Printf("[atrium] fresh start for %s failed too: %v", r.taskID, err)
		} else {
			d.publishTask(r.taskID)
			return
		}
	}

	// A card put down by hand stays where it was put.
	if t, err := d.st.Get(r.taskID); err == nil &&
		t.Status != store.StatusShelved && t.Status != store.StatusDone {
		if err := d.st.SetStatus(r.taskID, store.StatusDead); err != nil {
			log.Printf("[atrium] status after exit for %s: %v", r.taskID, err)
		}
	}
	d.publishTask(r.taskID)
	log.Printf("[atrium] supervised runner for %s exited with %d", r.taskID, code)
}

// stopSupervised gives every owned runner a chance to finish, then closes its
// terminal.
//
// A runner in the middle of writing a file deserves the chance to finish. It
// does not deserve to hold shutdown open indefinitely, so the wait is bounded
// and narrated, the same way the rest of shutdown is.
func (d *Daemon) stopSupervised(grace time.Duration) {
	live := d.sup.all()
	if len(live) == 0 {
		return
	}
	log.Printf("[atrium] stopping %d supervised runner(s), up to %s each...", len(live), grace)

	var wg sync.WaitGroup
	for _, r := range live {
		wg.Add(1)
		go func(r *runner) {
			defer wg.Done()
			windDown(r, grace, d.exitKeysFor(r.taskID))
		}(r)
	}
	wg.Wait()
	log.Printf("[atrium] all supervised runners stopped")
}

// stopOne winds a single runner down and waits for it. Returns whether atrium
// owned a runner for that card at all.
//
// Used by shelving, which stops a session without ending the work: the card and
// its resume id stay, so unshelving starts the same conversation again.
func (d *Daemon) stopOne(taskID string, grace time.Duration) bool {
	r := d.sup.get(taskID)
	if r == nil {
		return false
	}
	windDown(r, grace, d.exitKeysFor(taskID))
	return true
}

// exitKeysFor is how this card's runner is asked to exit.
//
// Per runner, because there is no common answer: a shell takes `exit`, claude
// takes control-d twice, ollama and codex take it once. Falls back to
// control-c then `exit`, which is what a shell understands and what atrium did
// before any of this was configurable.
func (d *Daemon) exitKeysFor(taskID string) [][]byte {
	fallback := [][]byte{{0x03}, []byte("exit\r\n")}
	t, err := d.st.Get(taskID)
	if err != nil || t.Runner == "" {
		return fallback
	}
	h, err := d.st.Harness(t.Runner)
	if err != nil {
		return fallback
	}
	if keys := h.ExitBytes(); len(keys) > 0 {
		return keys
	}
	return fallback
}

// windDown asks a runner to stop, then insists.
//
// `keys` is the runner's own way of being asked, one write per keystroke,
// because two control-d presses are not the same to a program reading a
// terminal as one write of two bytes. Each is given a moment to take effect
// before the next, so a runner that quits on the first is never sent the rest.
//
// Only when all of them are ignored does the terminal close, and only after
// that is the process killed. An agent mid-turn may be part way through
// writing a file, and yanking its terminal away loses that.
func windDown(r *runner, grace time.Duration, keys [][]byte) {
	// A runner with no terminal has nothing to write to and nothing to close.
	// Guarded rather than assumed, because reaching this with a partial runner
	// would panic inside shutdown, which is the worst place to panic.
	if r == nil || r.pty == nil {
		return
	}
	for _, k := range keys {
		_ = r.Write(k)
		select {
		case <-r.done:
			log.Printf("[atrium] runner for %s exited when asked", r.taskID)
			return
		case <-time.After(600 * time.Millisecond):
		}
	}

	select {
	case <-r.done:
		log.Printf("[atrium] runner for %s exited cleanly", r.taskID)
		return
	case <-time.After(grace):
	}

	// It did not take the hint. Close the terminal, which most processes treat
	// as a hangup.
	log.Printf("[atrium] runner for %s did not exit in %s, closing its terminal", r.taskID, grace)
	r.closePTY()
	select {
	case <-r.done:
		log.Printf("[atrium] runner for %s stopped", r.taskID)
	case <-time.After(2 * time.Second):
		log.Printf("[atrium] runner for %s is still up, killing it", r.taskID)
		if r.cmd.Process != nil {
			_ = r.cmd.Process.Kill()
		}
	}
}
