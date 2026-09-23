package daemon

import (
	"context"
	"encoding/json"
	"net/http/httptest"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// A LIVE RE-FIT RESTORES THE CURSOR ONLY WHEN THE PTY ACTUALLY MOVES, which is
// the room-side half of the room-set-change garble and the reason the board fix
// (a re-attach on a room flip) has to exist.
//
// The attach replay restores the cursor (see cursor_position_test.go). But a
// viewer that re-fits WHILE ATTACHED gets a cursor only if that re-fit moves the
// pty: the pty resize raises SIGWINCH and the runner repaints, and the repaint
// carries the move. The pty moves only when the agreed size (widest width,
// shortest height) changes, so a viewer that is not the binding one re-fits, the pty
// stays put, no SIGWINCH fires, nothing is sent, and that viewer is left showing
// its old cursor against a reflowed grid.
//
// Nothing here can make the runner repaint (the pty is a fake), so this asserts
// the DECISION the cursor rides on: whether the pty moved. A room flip on the
// board triggers exactly this re-fit, which is why the board re-attaches to
// replay the cursor rather than trusting the resize to carry it.
//
// ROOM-SIDE reproduction, PARKED. The fix that ships is the board's.

// keptOpenViewer attaches over the real socket, states its size, drains output
// in the background, and lets the test resize it and read what came back. A
// per-read cancel closes the whole coder/websocket conn, so one long-lived read
// runs for the socket's life.
type keptOpenViewer struct {
	c      *websocket.Conn
	cancel context.CancelFunc
	mu     sync.Mutex
	buf    strings.Builder
}

func attachKeptOpen(t *testing.T, wsURL string, cols, rows int) *keptOpenViewer {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		cancel()
		t.Fatalf("dial: %v", err)
	}
	v := &keptOpenViewer{c: c, cancel: cancel}
	go func() {
		for {
			_, data, err := c.Read(ctx)
			if err != nil {
				return
			}
			v.mu.Lock()
			v.buf.Write(data)
			v.mu.Unlock()
		}
	}()
	v.resize(t, cols, rows)
	t.Cleanup(func() { cancel(); c.CloseNow() })
	return v
}

func (v *keptOpenViewer) resize(t *testing.T, cols, rows int) {
	t.Helper()
	frame, _ := json.Marshal(attachIn{T: "resize", Cols: cols, Rows: rows})
	if err := v.c.Write(context.Background(), websocket.MessageText, frame); err != nil {
		t.Fatalf("resize: %v", err)
	}
}

func (v *keptOpenViewer) seen() string {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.buf.String()
}

// The move `textWithCursor` appends: an absolute `CSI row;col H` to the
// session's cursor, as the last thing in the replay. Its presence is what "the
// cursor was restored" means.
func hasCursorMove(s string) bool {
	return regexp.MustCompile(`\x1b\[\d+;\d+H$`).MatchString(s)
}

func TestALiveRefitRestoresTheCursorOnlyWhenThePtyMoves(t *testing.T) {
	d := testDaemon(t)
	// A parked cursor, not at the bottom: row 2 col 6 of three drawn rows.
	f := narrowSession(t, d, "refit", "line one\r\nline two\r\nline three\r\n\x1b[2;6H")

	srv := httptest.NewServer(d.ap.Handler())
	t.Cleanup(srv.Close)
	wsURL := "ws" + strings.TrimPrefix(srv.URL, "http") + "/v1/tasks/refit/attach"

	// The small viewer binds the pty at 140x20.
	small := attachKeptOpen(t, wsURL, 140, 20)
	time.Sleep(300 * time.Millisecond)

	// The large viewer attaches. Its initial replay restores the cursor, which is
	// the attach fix working: this is the baseline the re-fit is measured against.
	large := attachKeptOpen(t, wsURL, 200, 30)
	time.Sleep(500 * time.Millisecond)
	if !hasCursorMove(large.seen()) {
		t.Fatalf("the initial attach did not restore the cursor: %q", large.seen())
	}
	before := len(small.seen())
	ptyMovesBefore := len(f.resized())

	// NON-BINDING RE-FIT: the small viewer re-fits, but 200 is still the widest
	// and 20 still the shortest, so the pty does not move. No SIGWINCH, no
	// repaint, nothing sent. The board re-attaches to cover exactly this.
	small.resize(t, 130, 20)
	time.Sleep(500 * time.Millisecond)
	if delta := small.seen()[before:]; delta != "" {
		t.Fatalf("a non-binding re-fit sent bytes it should not have: %q", delta)
	}
	if moves := len(f.resized()); moves != ptyMovesBefore {
		t.Fatalf("a non-binding re-fit moved the pty: %+v", f.resized())
	}

	// BINDING RE-FIT: the large viewer re-fits narrower, and it IS the widest,
	// so the pty moves. A real runner would get SIGWINCH here and repaint, which
	// is how a binding viewer's cursor is carried without a re-attach.
	large.resize(t, 180, 28)
	time.Sleep(400 * time.Millisecond)
	sizes := f.resized()
	if len(sizes) == 0 || sizes[len(sizes)-1] != (viewport{180, 20}) {
		t.Fatalf("a binding re-fit did not move the pty: %+v", sizes)
	}
	// And every viewer is told, so the narrower one draws the new width.
	if !strings.Contains(small.seen(), `{"t":"size","cols":180,"rows":20}`) {
		t.Fatalf("the narrower viewer was not told the pty's new size: %q", small.seen()[before:])
	}
}
