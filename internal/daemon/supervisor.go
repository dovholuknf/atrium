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
// leaves a mark at the stream position where it took effect.
//
// The marks DESCRIBE the output, they do not gate it. An attaching viewer is
// given everything retained and told which widths are in it, because misplaced
// text can still be read and an empty pane cannot. See `Replay`, which carries
// the two versions of this that withheld history instead.
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

// Grow makes the buffer bigger, keeping everything it holds.
//
// GROWING ONLY, and that asymmetry is the whole reason this can exist. The
// size was read once at spawn, with the note that resizing a live ring means
// either dropping what it holds or copying it under the lock. Half of that is
// true: SHRINKING has to throw bytes away and is refused here. Growing copies
// the retained run into a bigger slice in order, keeps every width mark, and
// loses nothing.
//
// It is not free, and it does not need to be. It runs when somebody raises the
// setting and on the first attach after that, which is a person clicking a
// button rather than anything on the output path. The alternative was
// "restart the daemon for this to take effect", said to somebody who raised
// the limit BECAUSE they had just lost scrollback, and whose restart would
// then cost them the buffer they were trying to keep.
func (r *ringBuffer) Grow(size int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if size <= len(r.data) {
		return
	}
	// THE RAW RETAINED BYTES, not what `from` returns. `from` trims to a line
	// start so a snapshot never begins mid escape, and dropping those bytes
	// here would shift `retainedStart` without shifting the width marks
	// expressed against it. What goes in is byte for byte what was held.
	data := make([]byte, size)
	n := 0
	if r.full {
		n = copy(data, r.data[r.at:])
		n += copy(data[n:], r.data[:r.at])
	} else {
		n = copy(data, r.data[:r.at])
	}
	r.data = data
	r.at = n
	r.full = false
	// `written` is not touched. It is a stream position that the marks are
	// expressed in, and moving it would invalidate every one of them. What
	// changes is how far back `retainedStart` reaches, which is exactly the
	// point.
}

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
// NOTHING OUTSIDE A TEST CALLS THIS, and it is worth saying why rather than
// deleting it. It used to be how a runner's last words were read, and doing
// that meant copying the whole ring to find twelve lines. That caller takes
// `Tail` now, and an attaching viewer takes `Replay`, which says what the
// bytes were drawn for.
//
// Kept because "everything retained, unconditionally" is the plainest
// statement of what the buffer holds, and both of the others are checked
// against it.
func (r *ringBuffer) Snapshot() []byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.from(r.retainedStart())
}

// Tail returns at most the last n bytes held, oldest first.
//
// FOR READING, NOT FOR REPLAYING. The caller is a message with its escape
// sequences stripped out, so none of the width machinery applies and none of
// it is consulted.
//
// It exists because asking for the whole ring to read twelve lines is not a
// rounding error at this size. `Snapshot` copies everything retained, and
// `lastOutput` then makes a string of it, hands that to a regexp that builds
// another, and splits that. With the scrollback setting at its ceiling the
// peak cost of finding out why a runner exited was over a gigabyte of
// allocation, paid on every exit, and Go returns a heap grown that far to the
// operating system lazily. A resident set far above anything the process held
// is what that looks like from outside.
//
// Clamped to the oldest byte still held, exactly as `Snapshot` is, so a ring
// that has wrapped can never hand back bytes that were overwritten.
func (r *ringBuffer) Tail(n int) []byte {
	if n <= 0 {
		return nil
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	pos := r.written - int64(n)
	if start := r.retainedStart(); pos < start {
		pos = start
	}
	return r.from(pos)
}

// Replay returns everything retained, oldest first, and every width it was
// composed at.
//
// ALL OF IT, and the width marks now DESCRIBE the output instead of gating it.
// That reverses the rule this buffer was built around, so the reasoning is
// worth having in full.
//
// The hazard is real and has not changed. A terminal user interface asks how
// wide the terminal is and composes for that number: hard breaks at the column
// it was told, boxes drawn to it, absolute cursor moves to positions it worked
// out itself. Replay those bytes into a wider grid and the breaks land a third
// of the way across and paragraphs sit on top of each other.
//
// Two attempts to protect the reader from that both made things worse:
//
//   - **Only the run composed at the current width.** Every resize lays a mark
//     before the pty is told, so an attach at a new size asked for a run zero
//     bytes old and got nil. Dragging a window edge emptied an hour of
//     scrollback.
//   - **Then: the trailing run, whatever it was drawn for.** Better, and still
//     wrong, because a run ENDS AT EVERY MARK. A session resized twice hands
//     back only what it has drawn since the second resize, which for a quiet
//     agent is a couple of screens out of sixteen megabytes. The operator's
//     words were "scrollback is not fixed", and they were right.
//
// What both share is deciding on the reader's behalf that imperfect output is
// worth less than no output. It is not. Misplaced text can be read, scrolled
// past, and searched. An empty pane cannot. So everything held is handed over,
// the widths come with it, and the caller says what the reader is looking at.
//
// The widths are returned oldest first with consecutive repeats collapsed. A
// mark sitting at the write position describes output nobody has produced yet
// and is left out, which is the same rule `SetWidth` applies when it merges
// one.
// `wrapped` is whether this is the start of the stream or the point the buffer
// began overwriting itself. THE READER HAS TO BE ABLE TO TELL. A scrollback
// that stops is either all there was or the buffer's own limit, and those two
// call for completely different reactions: one is nothing to do about, the
// other is a number in the settings. Somebody who cannot tell them apart reads
// every short history as the limit and every long one as luck.
func (r *ringBuffer) Replay() (out []byte, widths []int, wrapped bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	start := r.retainedStart()
	out = r.from(start)
	if len(out) == 0 {
		return nil, nil, false
	}
	for _, m := range r.marks {
		if m.at >= r.written {
			continue
		}
		if len(widths) > 0 && widths[len(widths)-1] == m.cols {
			continue
		}
		widths = append(widths, m.cols)
	}
	if len(widths) == 0 {
		// Every mark sits at the end, so nothing describes these bytes but the
		// width in force. Which is the width they were drawn at.
		widths = []int{r.marks[len(r.marks)-1].cols}
	}
	return out, widths, start > 0
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
	// IS THE OPERATOR PART WAY THROUGH SOMETHING. Two facts, kept here rather
	// than in the browser.
	//
	// `midLine` is whether keystrokes have arrived since the last thing that
	// ends a line, and `lastTyped` is when the most recent one landed. Between
	// them they answer the only question that decides whether another session
	// may type into this terminal.
	//
	// THE DAEMON IS THE RIGHT PLACE and the board is not, even though the
	// board already tracks something similar for path completion. That copy is
	// per viewer, dies on reload, and would make a decision about the pty
	// depend on which tab happens to be open. This one sees every byte,
	// because atrium is the only way the operator can type into a supervised
	// session: every keystroke arrives over the attach websocket and goes
	// through `Write`. There is no second writer to miss.
	//
	// The INPUT side only. What the runner did with those bytes is not knowable
	// from here and does not matter: the question is whether the operator is
	// mid-thought, and only their own keystrokes answer it. Reading the
	// runner's output to guess at this is the line `B2-20` declines to cross,
	// and it would be a guess where this is a record.
	midLine   bool
	lastTyped time.Time
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
//
// THE LOOP IS THE POINT. An io.Writer is allowed to take fewer bytes than it
// was given and return no error, and this is the one funnel every keystroke,
// paste, message, note and action prompt passes through. Ignoring the count
// means atrium reports success with the tail of the input gone, and nothing
// downstream can tell, because the only party that knew was the line that
// threw the number away. `Say` makes that silence dangerous: it writes the
// text and then writes the Enter, so a short first write gets a HALF PROMPT
// SUBMITTED to an agent.
//
// NO SLEEP BETWEEN THE PARTS, and that is the constraint the obvious
// implementation breaks. The burst is load bearing. A TUI decides input is a
// paste rather than typing from how fast it arrives, `Say` depends on that,
// and the board sends a paste as one frame for the same reason. A pause here
// would split one paste into two and undo both. Continue immediately.
func (r *runner) Write(p []byte) error {
	for len(p) > 0 {
		n, err := r.pty.Write(p)
		if err != nil {
			return err
		}
		if n <= 0 {
			// No progress and no error. Looping again would spin forever, so
			// call it what it is rather than hang the caller.
			return io.ErrShortWrite
		}
		p = p[n:]
	}
	return nil
}

// noteOperatorTyped records that the PERSON sent these bytes.
//
// Called from the attach socket and from nowhere else, which is what makes it
// trustworthy: `Say` and the peer bus also reach `Write`, and counting their
// bytes here would have atrium deciding the operator was busy because atrium
// had just typed something.
//
// What ends a line: a carriage return or a newline submits it, and the two
// ways a line is thrown away are control-c and control-u. Everything else
// leaves something part written, including a backspace, because a line being
// edited down to nothing is still a line somebody is working on.
func (r *runner) noteOperatorTyped(p []byte) {
	if len(p) == 0 {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lastTyped = time.Now()
	for _, b := range p {
		switch b {
		case '\r', '\n':
			r.midLine = false
		case 0x03, 0x15: // control-c, control-u
			r.midLine = false
		default:
			r.midLine = true
		}
	}
}

// peerQuiet is how long after the operator's last keystroke a terminal is
// still considered theirs.
//
// Short, because the common case this exists for is an agent talking to an
// agent while nobody is at the keyboard, and a long window would make that the
// uncommon case. Long enough that a pause for thought between two commands is
// not read as having walked away.
const peerQuiet = 20 * time.Second

// howBusy says whether another session may type into this terminal now.
//
// Three answers, and they are the design rather than an implementation
// detail. See `handleTell`.
type peerRoom int

const (
	// peerFree is nobody attached, or attached and long since idle. Type it
	// and press Enter: an agent talking to an agent while the operator is
	// asleep is the whole case this feature exists for, and a message that
	// does not submit does nothing.
	peerFree peerRoom = iota
	// peerWatching is somebody attached who typed recently, with no part
	// written line. Type it, attributed, and DO NOT press Enter. The operator
	// considers the pty shared, so the text goes in, and submitting under
	// somebody's hands is a different act from putting text in front of them.
	peerWatching
	// peerMidLine is a part written line. Never type. This is the case the old
	// refusal protected and it stays protected.
	peerMidLine
)

func (r *runner) howBusy() peerRoom {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.midLine {
		return peerMidLine
	}
	if !r.lastTyped.IsZero() && time.Since(r.lastTyped) < peerQuiet {
		return peerWatching
	}
	return peerFree
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
// `widths` is every width the backlog was composed at, oldest first, and
// `wantCols` is the width the terminal is at now. A caller with more than one
// width, or one that is not `wantCols`, is holding output that will not land
// where it was drawn to: see `attach.go`, which says so on screen.
//
// Snapshot and subscription are taken together under the one lock, so a chunk
// arriving between them can neither be lost nor sent twice.
func (r *runner) subscribe() (backlog []byte, widths []int, wantCols int, wrapped bool, updates chan []byte) {
	ch := make(chan []byte, 64)
	r.mu.Lock()
	defer r.mu.Unlock()
	cols := r.buf.CurrentWidth()
	buf, widths, wrapped := r.buf.Replay()
	// THIS PROCESS ONLY. What the card held before the restart is on disk and
	// is NOT joined on here any more.
	//
	// It was, for an afternoon, and the result was confusing in a way that
	// took a while to name: a resumed session REPRINTS its own recent history,
	// so the carried bytes ended mid-conversation and then the same
	// conversation started again below the divider. Two copies of the last
	// hour with nothing on screen to say why.
	//
	// A terminal running claude shows one session's output. Atrium now matches
	// that, and the older bytes are fetched deliberately rather than pushed at
	// somebody who did not ask. See `carryover.go` and `handleOlderScrollback`.
	select {
	case <-r.done:
		close(ch)
		return buf, widths, cols, wrapped, ch
	default:
	}
	r.watchers[ch] = struct{}{}
	return buf, widths, cols, wrapped, ch
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

// ringCount is how many scrollback rings are live, counted separately because
// they are two different reasons for the same allocation: a runner atrium
// owns, and a shell somebody opened beside one.
func (s *supervisor) ringCount() (runners, shells int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.runners), len(s.shells)
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

// launchWidthFor is the width to open THIS CARD's terminal at.
//
// THE WIDTH IT WAS LAST AT, and falling back to `launchCols` for a card that
// has never had one. This is worth more than it looks.
//
// A fixed launch width means every restart injects a stretch of output drawn
// for a terminal nobody is sitting at. The session comes up at a hundred and
// twenty columns, draws its resumed conversation there, and a browser attaches
// a second later and resizes it to whatever the window really is. The
// scrollback then holds two hundred and forty seven columns, then a hundred
// and twenty, then two hundred and forty seven again, once per restart, and
// the middle stretch has hard line breaks a third of the way across.
//
// Flattening the replay cannot undo that. A cursor move can be dropped, a line
// break that is already in the bytes cannot, so the only fix is not to produce
// it: come up at the width the window was, and the resize a moment later is
// not a change at all. See `ringBuffer.SetWidth`, which then merges the two
// marks and leaves one run.
//
// Read off the card, written there at the wind-down. It lived in the
// scrollback carryover's header for one afternoon, which was convenient right
// up until the carryover stopped being replayed automatically: a width that
// only exists inside a file nobody reads by default is a width that quietly
// stops working.
func (d *Daemon) launchWidthFor(taskID string) int {
	if taskID == "" {
		return launchCols
	}
	t, err := d.st.Get(taskID)
	if err == nil && t.LastCols > 0 {
		return t.LastCols
	}
	return launchCols
}

// sizeAtLaunch puts a freshly opened terminal at the size this card was last
// looked at, or the launch default.
//
// A refusal is not worth failing a launch over: the terminal still works at
// whatever size it opened with, and the first viewer to attach resizes it.
func sizeAtLaunch(p pty.Pty, cols int) {
	if err := p.Resize(cols, launchRows); err != nil {
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
	// The width this card was last looked at, so a reopened session draws its
	// first screen for the window it is about to appear in. See
	// `launchWidthFor`: a fixed width here put a stretch of narrow output into
	// the scrollback on every single restart.
	cols := d.launchWidthFor(taskID)
	sizeAtLaunch(p, cols)
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
		buf:      newRing(api.ScrollbackBytes(d.st), cols),
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
	//
	// A BOUNDED READ. This asked for the whole ring, which at the scrollback
	// ceiling is half a gigabyte copied to find twelve lines, and then copied
	// twice more by `lastOutput`. See `ringBuffer.Tail`.
	tail := lastOutput(r.buf.Tail(tailBytes), 12)
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

	// And their scrollback goes to disk. NOT REPLAYED AUTOMATICALLY any more,
	// but kept, because it is the only record of what was on screen and the
	// board can ask for it. See `carryover.go` for why the automatic replay
	// went away.
	//
	// AFTER the wind-down, so whatever a session said on its way out is in it,
	// and bounded by its own budget so it cannot extend a shutdown that is
	// already bounded and narrated.
	d.saveCarryover(live)
	// WHICH TERMINALS WERE OPEN, so the next daemon opens them again. See
	// `reopen.go`.
	d.saveReopen(live)
	// AND HOW WIDE EACH ONE WAS, so the terminal that replaces it comes up the
	// size of the window it is about to appear in rather than at a fixed width
	// that something resizes a moment later. See `launchWidthFor`.
	for _, r := range live {
		if r == nil || r.buf == nil {
			continue
		}
		if err := d.st.SetLastCols(r.taskID, r.buf.CurrentWidth()); err != nil {
			log.Printf("[atrium] could not record the terminal width for %s: %v", r.taskID, err)
		}
	}
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
