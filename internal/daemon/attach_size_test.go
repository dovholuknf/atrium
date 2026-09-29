package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// THE PTY IS THE SIZE OF THE VIEWER THAT ATTACHED, over the real socket.
//
// The tests in attach_width_test.go drive `setViewport` directly. These go in
// through the websocket the board uses, so the frame handling, the floor and the
// ordering are all in the path. What they check is the thing the operator sees:
// the terminal the runner draws into is as big as the pane looking at it.

// heldAttach is an attach that stays open until closed, which is what a board
// tab does. It has said how big it is and read nothing.
type heldAttach struct {
	c      *websocket.Conn
	cancel context.CancelFunc
}

func (h *heldAttach) close() {
	h.c.CloseNow()
	h.cancel()
}

func holdAttach(t *testing.T, d *Daemon, taskID string, cols, rows int) *heldAttach {
	t.Helper()
	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(context.Background())
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/tasks/" + taskID + "/attach"
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		cancel()
		t.Fatalf("could not attach: %v", err)
	}
	c.SetReadLimit(64 << 20)
	h := &heldAttach{c: c, cancel: cancel}
	t.Cleanup(h.close)
	// Nobody else reads this socket, and the daemon writes a replay into it.
	go func() {
		for {
			if _, _, err := c.Read(ctx); err != nil {
				return
			}
		}
	}()
	frame, _ := json.Marshal(attachIn{T: "resize", Cols: cols, Rows: rows})
	if err := c.Write(ctx, websocket.MessageText, frame); err != nil {
		t.Fatalf("could not send a size: %v", err)
	}
	return h
}

// ptyIs waits for the pty to be `want`, and for the ring's own record of the
// size to agree with it. Both, because the ring's mark is what a later replay
// cuts on and the pty is what the runner draws into.
func ptyIs(t *testing.T, f *fakePTY, r *runner, want viewport) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		sizes := f.resized()
		cols, rows := r.buf.CurrentSize()
		if len(sizes) > 0 && sizes[len(sizes)-1] == want && cols == want.cols && rows == want.rows {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("want the pty at %dx%d, pty was resized to %+v and the ring says %dx%d",
				want.cols, want.rows, sizes, cols, rows)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// resizesAfterAMoment gives the daemon time to do something it should NOT do,
// and reports how many resizes had happened by the end of it.
func resizesAfterAMoment(f *fakePTY) int {
	time.Sleep(300 * time.Millisecond)
	return len(f.resized())
}

func sizedSession(t *testing.T, d *Daemon, taskID string, cols, rows int) (*fakePTY, *runner) {
	t.Helper()
	f := newFakePTY()
	r := &runner{
		taskID:   taskID,
		pty:      f,
		started:  time.Now(),
		buf:      newRingSized(1<<16, cols, rows),
		watchers: map[chan []byte]struct{}{},
		done:     make(chan struct{}),
	}
	d.sup.add(r)
	t.Cleanup(func() { f.Close() })
	return f, r
}

func TestTheAttachingViewersSizeBecomesThePTYs(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "size-attach", 120, 30)

	holdAttach(t, d, "size-attach", 150, 40)
	ptyIs(t, f, r, viewport{150, 40})
}

// A REATTACH AT THE SAME SIZE MOVES NOTHING. A resize to the size the pty already
// is raises SIGWINCH and every viewer repaints, so the second attach of a tab
// that was just switched away and back must not cost one.
func TestReattachingAtTheSameSizeDoesNotResize(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "size-reattach", 120, 30)

	first := holdAttach(t, d, "size-reattach", 140, 36)
	ptyIs(t, f, r, viewport{140, 36})
	first.close()
	// The detach of the only viewer leaves the pty as it was.
	before := resizesAfterAMoment(f)

	holdAttach(t, d, "size-reattach", 140, 36)
	ptyIs(t, f, r, viewport{140, 36})
	if after := resizesAfterAMoment(f); after != before {
		t.Fatalf("a reattach at the size the pty already was resized it again: %+v", f.resized())
	}
}

// A CARD REOPENED AFTER A RESTART starts its terminal at the width it was saved
// at, and the pane that attaches next is usually a different size. The pty has to
// end up at the pane's, not stay at the saved one, or the runner draws for a
// width nobody is looking at. `launchWidthFor` is what reopenSaved's launch asks.
func TestARestartedSessionFollowsThePaneNotTheSavedWidth(t *testing.T) {
	d := testDaemon(t)
	task := plainTask(t, d, "size-restart")
	if err := d.st.SetLastCols(task.ID, 160); err != nil {
		t.Fatal(err)
	}
	saved := d.launchWidthFor(task.ID)
	if saved != 160 {
		t.Fatalf("the terminal would open at %d, want the 160 the card was saved at", saved)
	}
	f, r := sizedSession(t, d, task.ID, saved, 30)

	holdAttach(t, d, task.ID, 132, 44)
	ptyIs(t, f, r, viewport{132, 44})
}

// TWO VIEWERS: THE WIDEST WIDTH AND THE SHORTEST HEIGHT. The pty follows the
// pair, a narrower second viewer moves nothing, and the wider one leaving hands
// the width back to the one that remains.
func TestASecondViewerSizesThePTYBetweenTheTwo(t *testing.T) {
	d := testDaemon(t)
	f, r := sizedSession(t, d, "size-two", 120, 30)

	holdAttach(t, d, "size-two", 130, 50)
	ptyIs(t, f, r, viewport{130, 50})

	// Narrower and shorter: the width stays, the height comes down to it.
	second := holdAttach(t, d, "size-two", 110, 34)
	ptyIs(t, f, r, viewport{130, 34})

	// A wider one takes the width, and leaving gives it back.
	third := holdAttach(t, d, "size-two", 200, 60)
	ptyIs(t, f, r, viewport{200, 34})
	third.close()
	ptyIs(t, f, r, viewport{130, 34})

	second.close()
	ptyIs(t, f, r, viewport{130, 50})
}
