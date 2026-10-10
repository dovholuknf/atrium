package daemon

import (
	"context"

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
