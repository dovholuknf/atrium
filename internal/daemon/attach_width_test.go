package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aymanbagabas/go-pty"
	"github.com/coder/websocket"
)

// ATTACHING WIDE TO A SESSION THAT RAN NARROW, end to end over the socket the
// board actually uses.
//
// The unit tests either side of this one cover the ring buffer on its own.
// What they cannot cover is the ORDER, which is half the bug: a viewer's size
// arrives as a frame after the socket is up, so a daemon that writes the
// backlog first has already decided what to replay using the size the last
// viewer left behind.

// fakePTY is a terminal that records the sizes it is asked for and swallows
// everything typed at it.
//
// A real pseudo terminal would need a real process producing real output at a
// real width, which is a slow test of the operating system rather than a test
// of what to replay.
type fakePTY struct {
	mu    sync.Mutex
	sizes []viewport
	// What was typed INTO it, which the peer bus tests read back. A terminal
	// that swallows its input cannot answer whether a message was submitted,
	// and whether Enter was pressed is the whole difference between two of the
	// three states in `tellByTyping`.
	in     []byte
	closed chan struct{}
}

func newFakePTY() *fakePTY { return &fakePTY{closed: make(chan struct{})} }

// Read blocks until close, which is what a quiet terminal does. Nothing in
// these tests reads from it: output is put into the ring buffer directly, the
// way the supervisor's reader would.
func (f *fakePTY) Read(p []byte) (int, error) { <-f.closed; return 0, context.Canceled }

func (f *fakePTY) Write(p []byte) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.in = append(f.in, p...)
	return len(p), nil
}

// written is everything typed into it so far.
func (f *fakePTY) written() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return string(f.in)
}

func (f *fakePTY) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	select {
	case <-f.closed:
	default:
		close(f.closed)
	}
	return nil
}

func (f *fakePTY) Resize(cols, rows int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.sizes = append(f.sizes, viewport{cols, rows})
	return nil
}

func (f *fakePTY) resized() []viewport {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]viewport(nil), f.sizes...)
}

func (f *fakePTY) Name() string                                               { return "fake-pty" }
func (f *fakePTY) Fd() uintptr                                                { return 0 }
func (f *fakePTY) Command(string, ...string) *pty.Cmd                         { return nil }
func (f *fakePTY) CommandContext(context.Context, string, ...string) *pty.Cmd { return nil }

// narrowSession is a supervised runner that has produced an hour of output at
// eighty columns and is still running.
func narrowSession(t *testing.T, d *Daemon, taskID string, output string) *fakePTY {
	t.Helper()
	f := newFakePTY()
	r := &runner{
		taskID:   taskID,
		pty:      f,
		started:  time.Now(),
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	if _, err := r.buf.Write([]byte(output)); err != nil {
		t.Fatal(err)
	}
	d.sup.add(r)
	t.Cleanup(func() { f.Close() })
	return f
}

// attachAs opens the real attach handler over a real websocket, says how big
// this viewer is, and returns everything written back within a moment.
func attachAs(t *testing.T, d *Daemon, taskID string, cols, rows int) string {
	t.Helper()
	return attachPath(t, d, "/v1/tasks/"+taskID+"/attach", cols, rows)
}

// attachPath is attachAs for any attach URL, so a shell can be reached too.
func attachPath(t *testing.T, d *Daemon, path string, cols, rows int) string {
	t.Helper()
	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + path
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("could not attach: %v", err)
	}
	defer c.CloseNow()

	// The board sends this from `onopen`, which is after the upgrade. That is
	// the whole ordering problem being tested.
	frame, _ := json.Marshal(attachIn{T: "resize", Cols: cols, Rows: rows})
	if err := c.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("could not send a size: %v", err)
	}

	var got strings.Builder
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		read, stop := context.WithTimeout(ctx, 300*time.Millisecond)
		_, data, err := c.Read(read)
		stop()
		if err != nil {
			break
		}
		got.Write(data)
	}
	return got.String()
}

// ATTACHING WIDE TO NARROW HISTORY HANDS IT OVER, UNANNOUNCED.
//
// The history is replayed whatever width it was drawn at, and the width-mismatch
// note that used to sit above it is gone: the operator found it noise and it
// fired on every reattach after a hub restart. The bytes still arrive.
func TestAttachingWideReplaysNarrowOutputWithoutANote(t *testing.T) {
	d := testDaemon(t)
	narrow := "an hour of output composed for eighty columns\n"
	f := narrowSession(t, d, "wide-attach", narrow)

	got := attachAs(t, d, "wide-attach", 200, 50)

	if !strings.Contains(got, "eighty columns") {
		t.Fatalf("withheld the history: %q", got)
	}
	if strings.Contains(got, "columns wide") || strings.Contains(got, "resized while it ran") {
		t.Fatalf("emitted the width note that was removed: %q", got)
	}
	// And the terminal was told the size the viewer asked for, so the runner
	// is already repainting into the space the drop left.
	sizes := f.resized()
	if len(sizes) == 0 || sizes[len(sizes)-1] != (viewport{200, 50}) {
		t.Fatalf("the terminal was not resized for this viewer: %+v", sizes)
	}
}

// The ordinary attach, which none of this is allowed to make worse: same
// width, so the whole buffer is replayed and nothing is announced.
func TestAttachingAtTheSameWidthStillShowsTheScrollback(t *testing.T) {
	d := testDaemon(t)
	narrowSession(t, d, "same-width", "an hour of output composed for eighty columns\n")

	got := attachAs(t, d, "same-width", 80, 24)

	if !strings.Contains(got, "eighty columns") {
		t.Fatalf("threw away scrollback that renders correctly: %q", got)
	}
}

// A SECOND VIEWER MUST NOT COST THE FIRST ONE ITS SCREEN.
//
// The smallest attached viewer decides, so a narrow window joining a wide
// session resizes the terminal. The narrow viewer still gets the history.
func TestASecondNarrowerViewerStillGetsTheHistory(t *testing.T) {
	d := testDaemon(t)
	f := narrowSession(t, d, "two-viewers", "drawn at eighty columns\n")

	// The wide one attaches first and gets everything.
	if got := attachAs(t, d, "two-viewers", 200, 24); !strings.Contains(got, "eighty columns") {
		t.Fatalf("the first viewer lost its scrollback: %q", got)
	}
	// It stays attached in no meaningful sense here: `attachAs` returns after
	// its socket closes, and dropping a viewport gives the size back. What
	// matters is that the terminal now moves to 140 columns for the second,
	// which is above the width floor, and that viewer still receives the
	// scrollback.
	if got := attachAs(t, d, "two-viewers", 140, 20); !strings.Contains(got, "eighty columns") {
		t.Fatalf("the narrow viewer lost its scrollback: %q", got)
	}
	if sizes := f.resized(); len(sizes) == 0 || sizes[len(sizes)-1].cols != 140 {
		t.Fatalf("the terminal did not follow the narrow viewer: %+v", sizes)
	}
}

// THE REPLAY ARRIVES UNABLE TO ERASE ITSELF, over the real socket.
//
// The last and worst version of this bug: the daemon handed over two megabytes
// of scrollback, every byte arrived, and the browser showed two screens,
// because the history is full of absolute cursor moves and the moves landed on
// the history. Measured on one card, 33,957 of them in 1.6MB.
//
// So the check is not that the bytes were sent. It is that what was sent
// cannot destroy itself once xterm parses it.
func TestTheReplayCannotEraseItself(t *testing.T) {
	d := testDaemon(t)
	// An hour of work, then the redraw claude-code emits on every turn.
	history := strings.Repeat("a line of real work\r\n", 200) +
		"\x1b[H\x1b[2J\x1b[38;5;244mthe current screen\x1b[0m\r\n"
	narrowSession(t, d, "flat", history)

	got := attachAs(t, d, "flat", 80, 24)

	if n := strings.Count(got, "a line of real work"); n != 200 {
		t.Fatalf("replayed %d of 200 lines of history", n)
	}
	if strings.Contains(got, "\x1b[H") || strings.Contains(got, "\x1b[2J") {
		t.Fatal("sent the moves that land on the history instead of after it")
	}
	if !strings.Contains(got, "the current screen") {
		t.Fatal("dropped the newest output along with the moves")
	}
	// Colour is the one thing kept, because it cannot move or erase anything.
	// The screen model re-emits it canonically from a reset (`ESC [ 0 ; ...`),
	// which is why the assertion is the 244 colour rather than the bare form the
	// runner wrote. Before the preamble was removed this passed on the divider's
	// own grey, which hid that the content colour is emitted this way.
	if !strings.Contains(got, "38;5;244m") {
		t.Fatal("stripped the colour as well")
	}
	// And the history/live divider is GONE. It used to sit under the flattened
	// history so the first live redraw did not read as corruption; clint found
	// it noise on every reattach and asked to ditch the whole preamble.
	if strings.Contains(got, "live from here") || strings.Contains(got, "everything above is history") {
		t.Fatalf("emitted the attach preamble that was removed: %q", got)
	}
}

// A NARROWER CONSOLE NEVER TOUCHES THE PTY, so it cannot churn the other
// viewers or pile a reprint into their scrollback. The pty follows the widest
// viewer and moves only when that widest actually changes.
func TestANarrowerViewerNeverResizesThePTY(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{
		taskID:   "decouple",
		pty:      f,
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	// The wide viewer is the binding one and matches the launch size, so it is
	// already a no-op.
	if err := r.setViewport("wide", 80, 24); err != nil {
		t.Fatal(err)
	}
	// A narrower viewer joins and then drags narrower still. Neither is the
	// widest, so the pty is never asked to resize.
	_ = r.setViewport("narrow", 60, 24)
	_ = r.setViewport("narrow", 40, 24)
	if sizes := f.resized(); len(sizes) != 0 {
		t.Fatalf("a narrower viewer churned the pty: %+v", sizes)
	}
	// The binding viewer resizing IS applied, exactly once.
	_ = r.setViewport("wide", 120, 24)
	if sizes := f.resized(); len(sizes) != 1 || sizes[len(sizes)-1] != (viewport{120, 24}) {
		t.Fatalf("the binding viewer's resize was not applied once: %+v", sizes)
	}
	// The narrower viewer detaching does not move the pty either.
	r.dropViewport("narrow")
	if sizes := f.resized(); len(sizes) != 1 {
		t.Fatalf("a narrower viewer detaching churned the pty: %+v", sizes)
	}
}

// A WIDER READER MOVES THE PTY, and when it leaves the pty follows back to the
// readers that remain.
func TestAWiderViewerMovesThePTYAndReleasesIt(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{
		taskID:   "wider",
		pty:      f,
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	_ = r.setViewport("pane", 80, 24)  // matches the launch width, a no-op
	_ = r.setViewport("desk", 200, 24) // binding, so it widens the pty
	if sizes := f.resized(); len(sizes) != 1 || sizes[len(sizes)-1] != (viewport{200, 24}) {
		t.Fatalf("the wide reader did not widen the pty: %+v", sizes)
	}
	r.dropViewport("desk")
	if sizes := f.resized(); len(sizes) != 2 || sizes[len(sizes)-1] != (viewport{80, 24}) {
		t.Fatalf("the pty did not follow back when the binding viewer left: %+v", sizes)
	}
}

// EVERY ATTACH IS TOLD THE PTY'S SIZE WHEN IT MOVES, so a narrower board can
// draw the width it did not ask for. See `sizeChanged`.
func TestAResizeWakesSizeWatchers(t *testing.T) {
	f := newFakePTY()
	t.Cleanup(func() { f.Close() })
	r := &runner{
		taskID:   "wake",
		pty:      f,
		buf:      newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	wake := r.sizeChanged()
	_ = r.setViewport("pane", 80, 24) // a no-op wakes nobody
	select {
	case <-wake:
		t.Fatal("a resize to the size already in force woke the watchers")
	default:
	}
	_ = r.setViewport("pane", 132, 24)
	select {
	case <-wake:
	default:
		t.Fatal("a real resize did not wake the watchers")
	}
	if cols, _ := r.buf.CurrentSize(); cols != 132 {
		t.Fatalf("woken before the new size was readable: %d", cols)
	}
}

// A SESSION RESIZED WHILE IT RAN HANDS BACK BOTH SIDES, over the socket the
// board actually uses.
//
// The unit test on the ring proves the bytes are there. This proves nothing
// between the buffer and the browser drops them again, which is where the
// first attempt at this fix went wrong: `subscribe` asked for one run of
// output, and the run WAS the whole contract.
func TestAResizedSessionReplaysEverythingOverTheSocket(t *testing.T) {
	d := testDaemon(t)
	f := narrowSession(t, d, "resized", "drawn before the drag\n")
	// The drag, and something drawn after it.
	run := d.sup.get("resized")
	run.buf.SetWidth(120)
	run.buf.Write([]byte("drawn after the drag\n"))

	got := attachAs(t, d, "resized", 200, 50)

	if !strings.Contains(got, "drawn before the drag") {
		t.Fatalf("the output from before the resize never arrived: %q", got)
	}
	if !strings.Contains(got, "drawn after the drag") {
		t.Fatalf("the output from after the resize never arrived: %q", got)
	}
	if strings.Contains(got, "resized while it ran") {
		t.Fatalf("emitted the width note that was removed: %q", got)
	}
	if sizes := f.resized(); len(sizes) == 0 || sizes[len(sizes)-1].cols != 200 {
		t.Fatalf("the terminal was not resized for this viewer: %+v", sizes)
	}
}
