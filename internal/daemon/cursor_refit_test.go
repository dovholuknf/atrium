package daemon

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"

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
