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

// RETAINED OUTPUT IS ONLY MEANINGFUL AT THE WIDTH IT WAS WRITTEN AT.
//
// A terminal user interface does not emit text and leave the wrapping to
// whoever reads it. It asks how wide the terminal is and then composes for
// that number: hard line breaks at the column it was told, boxes drawn to it,
// and absolute cursor moves to a row and column it has worked out itself.
// Those bytes go into the ring exactly as sent.
//
// Replay them into a wider grid and they do not come out merely ragged. The
// hard breaks land a third of the way across a two hundred column window, and
// the absolute moves put the next paragraph on top of the last one. That is
// the unreadable attach: sixty column text in a two hundred column window,
// diffs overlapping themselves, and two thirds of the screen empty.
//
// So the width is recorded WITH the bytes. Every change of the agreed size
// leaves a mark at the stream position where it took effect, and an attaching
// viewer is only given the run of output that was composed at the width its
// terminal is at now. What was composed at some earlier width is dropped and
// said to be dropped, which loses history nobody could have read anyway.
//
// COLUMNS ONLY, not rows. Width is what decides how the bytes were composed:
// wrapping, boxes and column positions are all a function of it. A height
// mismatch moves a repaint up or down the screen and the runner's next draw
// puts it right, so keying on rows as well would throw away history to buy
// very little.

// widthMark is where the terminal became `cols` wide, in stream position.
type widthMark struct {
	at   int64
	cols int
}

// ringBuffer keeps the last N bytes written to it, and the widths they were
// written at.
type ringBuffer struct {
	mu   sync.Mutex
	data []byte
	full bool
	at   int
	// written counts every byte ever handed to Write, including bytes long
	// since overwritten. A stream position stays meaningful after the buffer
	// has wrapped over what it names, which a ring index does not.
	written int64
	// marks is every width this output was composed at, oldest first, and is
	// never empty: a buffer starts at the size its terminal was opened with.
	marks []widthMark
}

func newRing(size, cols int) *ringBuffer {
	return &ringBuffer{data: make([]byte, size), marks: []widthMark{{at: 0, cols: cols}}}
}

func (r *ringBuffer) Write(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := len(p)
	r.written += int64(n)
	defer r.forgetOldMarks()
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

// SetWidth records that everything written from here on was composed for a
// terminal this many columns wide.
//
// A repeat of the width already in force is not a mark. Two viewers agreeing
// on eighty columns must not split the run of output they can both read.
func (r *ringBuffer) SetWidth(cols int) {
	if cols <= 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if last := r.marks[len(r.marks)-1]; last.cols == cols {
		return
	}

	// A WIDTH NOTHING WAS WRITTEN AT DESCRIBES NOTHING, so it is not a mark.
	//
	// This cost an hour of somebody's scrollback. Popping a window out and
	// closing it again changes the agreed size twice in a moment, because the
	// smallest attached viewer decides. If no output arrived in between, the
	// buffer went from two hundred columns to eighty and back to two hundred
	// with every byte still composed for two hundred, and a mark at each step
	// cut the replay to whatever came after the last one. The operator
	// reattached to an hour-old session and found one page.
	//
	// So a mark laid at the position the previous one sits at REPLACES it, and
	// if that makes the last two agree they merge, which puts the earlier run
	// back. Nothing is guessed: the bytes before that position really were
	// composed at the width now in force.
	if last := len(r.marks) - 1; r.marks[last].at == r.written && last > 0 {
		r.marks = r.marks[:last]
		if r.marks[len(r.marks)-1].cols == cols {
			return
		}
	}
	r.marks = append(r.marks, widthMark{at: r.written, cols: cols})
	r.forgetOldMarks()
}

// forgetOldMarks drops marks the buffer has wrapped past.
//
// Bounded on purpose. A session resized a thousand times over a day would
// otherwise carry a thousand marks describing bytes that are long gone. The
// mark covering the oldest retained byte is kept whatever its position, since
// it is the one that says what that byte was composed for.
func (r *ringBuffer) forgetOldMarks() {
	start := r.retainedStart()
	keep := 0
	for i, m := range r.marks {
		if m.at <= start {
			keep = i
		}
	}
	if keep > 0 {
		r.marks = append(r.marks[:0], r.marks[keep:]...)
	}
}

// retained is how many bytes are actually held.
func (r *ringBuffer) retained() int {
	if r.full {
		return len(r.data)
	}
	return r.at
}

// retainedStart is the stream position of the oldest byte still held.
func (r *ringBuffer) retainedStart() int64 { return r.written - int64(r.retained()) }

// CurrentWidth is the width output is being composed at right now.
//
// The ring is asked rather than the viewports, because it is the same answer
// from the same place the bytes were filed under. A session everybody has
// detached from keeps the last size it was given, which no set of attached
// viewports can say.
func (r *ringBuffer) CurrentWidth() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.marks[len(r.marks)-1].cols
}

// Snapshot returns the retained output, oldest first, whatever width it was
// composed at.
//
// For reading a runner's last words rather than for putting on a screen. An
// attaching viewer wants SnapshotAt.
func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.from(r.retainedStart())
}

// SnapshotAt returns the retained output that was composed for a terminal
// `cols` wide, and whether anything older than that was left out.
//
// THE TRAILING RUN ONLY. A session that went eighty, then two hundred, then
// eighty again holds two stretches of eighty column output with something
// unreadable between them, and splicing the two together would join text
// across a hole. The run that reaches the end of the stream is the one that
// continues into what the runner draws next, so it is the only one worth
// sending.
func (r *ringBuffer) SnapshotAt(cols int) (out []byte, dropped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.retainedStart()
	last := r.marks[len(r.marks)-1]
	if last.cols != cols {
		// Everything held was composed for a terminal of another size.
		return nil, r.retained() > 0
	}
	if last.at > start {
		return r.from(last.at), true
	}
	return r.from(start), false
}

// from returns the retained bytes at and after a stream position, oldest
// first, trimmed to somewhere it is safe to start reading.
//
// Callers hold the lock.
func (r *ringBuffer) from(pos int64) []byte {
	start := r.retainedStart()
	if pos < start {
		pos = start
	}
	n := int(r.written - pos)
	if n <= 0 || len(r.data) == 0 {
		return nil
	}
	out := make([]byte, 0, n)
	// The oldest retained byte sits at `at` once the buffer has wrapped, and
	// at zero before that.
	begin := r.at
	if !r.full {
		begin = 0
	}
	begin = (begin + int(pos-start)) % len(r.data)
	if begin+n <= len(r.data) {
		out = append(out, r.data[begin:begin+n]...)
	} else {
		out = append(out, r.data[begin:]...)
		out = append(out, r.data[:n-(len(r.data)-begin)]...)
	}
	if pos == 0 {
		// The true start of the stream. Nothing was cut, so nothing is partial.
		return out
	}
	return fromLineStart(out)
}

// fromLineStart drops everything before the first line ending.
//
// A SNAPSHOT CAN BEGIN ANYWHERE. The write cursor is a byte offset with no
// idea what is at it, and a mark lands between two writes of a pty read that
// is itself an arbitrary 8KB of a stream. Either can fall halfway through an
// escape sequence or halfway through a multi byte rune.
//
// Both are worse than losing text. A severed escape has its introducer eaten,
// so its tail arrives as printable characters: `[2;34H` typed onto the screen.
// A severed rune renders as a replacement character and, worse, can eat the
// bytes after it.
//
// Fixed here rather than in Write, because Write has to keep taking bytes as
// fast as the runner produces them and cannot afford to parse them. LOSING A
// LINE BEATS SHIPPING A BROKEN ESCAPE.
//
// A line feed is the boundary because it cannot appear inside either: escape
// sequence parameter and intermediate bytes are 0x20 to 0x3F, final bytes are
// 0x40 to 0x7E, and every byte of a multi byte rune has its top bit set. The
// exception is an operating system command carrying a newline inside a window
// title, which no runner here sends.
//
// Nothing at all when there is no line ending in the whole snapshot. That is
// megabytes of one redrawing line, which has no safe starting point in it and
// is about to be redrawn again anyway.
func fromLineStart(b []byte) []byte {
	for i, c := range b {
		if c == '\n' {
			return b[i+1:]
		}
	}
	return nil
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
	// carried is what the card's terminal held before the last restart, and
	// the width it was composed at. See `carryover.go`: a restart destroys
	// every ring, so without this a resumed fixture attaches to a terminal
	// that starts at "just now".
	//
	// Kept beside the ring rather than loaded into it. The ring is a stream
	// with one set of width marks and these bytes came from another process,
	// so writing them in would file the new runner's first output under the
	// old runner's width.
	carried *carryover
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
	// Marked BEFORE the resize, so the first byte drawn at the new width is
	// already on the new side of the mark. The other order leaves a repaint
	// filed under the width it replaced, which is the whole bug.
	r.buf.SetWidth(agreed.cols)
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
	r.buf.SetWidth(agreed.cols)
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
//
// The backlog is only ever the run of output composed at the width the
// terminal is at now, and `dropped` says whether there was older output that
// was not. See the ring buffer's own comment: replaying bytes composed for
// another width is what makes an attach unreadable.
//
// Snapshot and subscription are taken together under the one lock, so a chunk
// arriving between them can neither be lost nor sent twice.
func (r *runner) subscribe() (backlog []byte, dropped bool, updates chan []byte) {
	ch := make(chan []byte, 64)
	r.mu.Lock()
	defer r.mu.Unlock()
	cols := r.buf.CurrentWidth()
	buf, cut := r.buf.SnapshotAt(cols)
	// And what the card held before the restart, WHEN THE WIDTH STILL AGREES.
	//
	// The same rule the ring applies to its own history, applied to bytes that
	// outlived the process which produced them: output composed for another
	// size cannot be redrawn here, so it is left out rather than replayed on
	// top of itself.
	//
	// This is the one place a join is made across a gap, which the ring itself
	// refuses to do. It is allowed here because the gap is a RESTART rather
	// than a stretch of unreadable output: the two sides are two processes,
	// the boundary is real, and it is drawn on screen instead of being hidden.
	if r.carried != nil && r.carried.cols == cols {
		joined := make([]byte, 0, len(r.carried.bytes)+len(carryDivider)+len(buf))
		joined = append(joined, r.carried.bytes...)
		joined = append(joined, carryDivider...)
		buf = append(joined, buf...)
	}
	select {
	case <-r.done:
		close(ch)
		return buf, cut, ch
	default:
	}
	r.watchers[ch] = struct{}{}
	return buf, cut, ch
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
	// claimed is every card that has already been offered the scrollback the
	// last daemon left for it. See `adoptCarryover`: the offer is made once
	// per card per daemon, and this is the record of it having been made,
	// which has to outlive the runner that took it.
	claimed map[string]bool
}

func newSupervisor() *supervisor {
	return &supervisor{
		runners: map[string]*runner{}, shells: map[string]*runner{},
		claimed: map[string]bool{},
	}
}

// claimCarry answers true exactly once per card.
func (s *supervisor) claimCarry(taskID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.claimed[taskID] {
		return false
	}
	s.claimed[taskID] = true
	return true
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

// The size a terminal is opened at, before anybody has attached to it.
//
// CHOSEN RATHER THAN INHERITED. A pseudo terminal opens at whatever its
// platform defaults to, which on Windows is eighty by twenty five. A session
// launched with nobody watching then composes an hour of output for a width
// nothing in atrium ever recorded, and the whole point of keeping the width
// with the bytes is that the first byte's width is known.
//
// Wide rather than narrow, because a browser pane on an ordinary screen is
// nearer a hundred and twenty columns than eighty, and output survives a
// later attach only when the two agree.
const (
	launchCols = 120
	launchRows = 30
)

// sizeAtLaunch puts a freshly opened terminal at the launch size.
//
// A refusal is not worth failing a launch over: the terminal still works at
// whatever size it opened with, and the first viewer to attach resizes it.
func sizeAtLaunch(p pty.Pty) {
	if err := p.Resize(launchCols, launchRows); err != nil {
		log.Printf("[atrium] could not set the launch terminal size: %v", err)
	}
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
	sizeAtLaunch(p)
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
		buf:      newRing(api.ScrollbackBytes(d.st), launchCols),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	// BEFORE `add`, which is the moment an attach can find this runner. A
	// viewer that arrived between the two would be sent the new terminal's
	// first bytes and nothing before them, which is the bug being fixed.
	d.adoptCarryover(r)
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

	// And their scrollback goes to disk, so the next daemon can hand it back.
	// AFTER the wind-down, so whatever a session said on its way out is in it,
	// and bounded by its own budget so it cannot extend a shutdown that is
	// already bounded and narrated. See `carryover.go`.
	d.saveCarryover(live)
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
