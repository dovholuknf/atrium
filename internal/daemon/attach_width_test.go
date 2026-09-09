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
	mu     sync.Mutex
	sizes  []viewport
	closed chan struct{}
}

func newFakePTY() *fakePTY { return &fakePTY{closed: make(chan struct{})} }

// Read blocks until close, which is what a quiet terminal does. Nothing in
// these tests reads from it: output is put into the ring buffer directly, the
// way the supervisor's reader would.
func (f *fakePTY) Read(p []byte) (int, error) { <-f.closed; return 0, context.Canceled }

func (f *fakePTY) Write(p []byte) (int, error) { return len(p), nil }

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
	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/tasks/" + taskID + "/attach"
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

// ATTACHING WIDE TO NARROW HISTORY HANDS IT OVER, LABELLED.
//
// This asserted the opposite until the operator resized a window and lost an
// hour of scrollback to a rule that was protecting them from imperfect
// rendering. The bytes really do land in the wrong columns; the fix is to say
// so rather than to withhold them, because the alternative on screen is
// nothing and nothing cannot be read either.
func TestAttachingWideReplaysNarrowOutputWithALabel(t *testing.T) {
	d := testDaemon(t)
	narrow := "an hour of output composed for eighty columns\n"
	f := narrowSession(t, d, "wide-attach", narrow)

	got := attachAs(t, d, "wide-attach", 200, 50)

	if !strings.Contains(got, "eighty columns") {
		t.Fatalf("withheld the history instead of labelling it: %q", got)
	}
	if !strings.Contains(got, "columns wide") {
		t.Fatalf("handed over output drawn elsewhere without saying so: %q", got)
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
	if strings.Contains(got, "columns wide") {
		t.Fatalf("announced a width mismatch that did not happen: %q", got)
	}
}

// A SECOND VIEWER MUST NOT COST THE FIRST ONE ITS SCREEN.
//
// The smallest attached viewer decides, so a narrow window joining a wide
// session resizes the terminal. The narrow viewer still gets the history, with
// the line saying what it was drawn for.
func TestASecondNarrowerViewerIsToldWhatItIsLookingAt(t *testing.T) {
	d := testDaemon(t)
	f := narrowSession(t, d, "two-viewers", "drawn at eighty columns\n")

	// The wide one attaches first and gets everything.
	if got := attachAs(t, d, "two-viewers", 80, 24); !strings.Contains(got, "eighty columns") {
		t.Fatalf("the first viewer lost its scrollback: %q", got)
	}
	// It stays attached in no meaningful sense here: `attachAs` returns after
	// its socket closes, and dropping a viewport gives the size back. What
	// matters is that the terminal now moves to forty columns for the second.
	if got := attachAs(t, d, "two-viewers", 40, 20); !strings.Contains(got, "columns wide") {
		t.Fatalf("the narrow viewer was sent eighty column output unlabelled: %q", got)
	}
	if sizes := f.resized(); len(sizes) == 0 || sizes[len(sizes)-1].cols != 40 {
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
	if !strings.Contains(got, "\x1b[38;5;244m") {
		t.Fatal("stripped the colour as well")
	}
	// And the boundary is drawn, or the first live redraw reads as the history
	// having been corrupted.
	if !strings.Contains(got, "live from here") {
		t.Fatalf("did not mark where the flattened history ends: %q", got)
	}
}

// THE NOTE IS THE WHOLE PAYMENT for handing over output drawn elsewhere, so it
// has to be right in each case that says something and silent in the two that
// have nothing to say.
func TestWidthNoteSaysWhatTheReaderIsLookingAt(t *testing.T) {
	cases := []struct {
		name   string
		widths []int
		want   int
		expect string
		absent bool
	}{
		{name: "the ordinary attach", widths: []int{80}, want: 80, absent: true},
		{name: "no backlog at all", widths: nil, want: 80, absent: true},
		{name: "all of it drawn elsewhere", widths: []int{80}, want: 200,
			expect: "80 columns wide and this one is 200"},
		{name: "resized while it ran", widths: []int{80, 200, 120}, want: 200,
			expect: "drawn at 80, 200, 120 columns and this terminal is 200"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := widthNote(c.widths, c.want)
			if c.absent {
				if got != "" {
					t.Fatalf("said something when there was nothing to say: %q", got)
				}
				return
			}
			if !strings.Contains(got, c.expect) {
				t.Fatalf("wanted %q in the note, got %q", c.expect, got)
			}
		})
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
	if !strings.Contains(got, "resized while it ran") {
		t.Fatalf("handed over output drawn at two widths without saying so: %q", got)
	}
	if sizes := f.resized(); len(sizes) == 0 || sizes[len(sizes)-1].cols != 200 {
		t.Fatalf("the terminal was not resized for this viewer: %+v", sizes)
	}
}
