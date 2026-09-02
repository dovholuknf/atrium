package daemon

import (
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/aymanbagabas/go-pty"
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

// scrollback is how much recent output is kept per runner, in bytes.
//
// Sized in bytes rather than lines because one line of a progress bar can be
// enormous. This is a convenience for someone attaching, not a transcript: the
// durable record of what happened is the event log, and writing every byte a
// runner emits to SQLite would grow without bound to buy very little.
const scrollback = 256 * 1024

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

	mu       sync.Mutex
	buf      *ringBuffer
	watchers map[chan []byte]struct{}
	done     chan struct{}
	exitOnce sync.Once
}

// Write sends keystrokes to the runner. Nothing arbitrates between two
// attachers typing at once, which is the same situation as two hands on one
// keyboard.
func (r *runner) Write(p []byte) error {
	_, err := r.pty.Write(p)
	return err
}

func (r *runner) Resize(cols, rows int) error { return r.pty.Resize(cols, rows) }

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
type supervisor struct {
	mu      sync.Mutex
	runners map[string]*runner
}

func newSupervisor() *supervisor { return &supervisor{runners: map[string]*runner{}} }

func (s *supervisor) get(taskID string) *runner {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.runners[taskID]
}

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

// spawnPTY starts a runner under a pseudo terminal and returns its pid.
func (d *Daemon) spawnPTY(taskID, cmdName string, args []string, cwd string, env []string) (int, error) {
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
		buf:      newRing(scrollback),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	d.sup.add(r)

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
	_ = r.pty.Close()
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
	_ = r.pty.Close()
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
