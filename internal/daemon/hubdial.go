package daemon

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// A connection to the hub, dialled by the room for a command on this machine.
//
// THE ROOM HOLDS THE ONLY LADDER. A command that dialled the hub itself would load the ziti identity and build a
// context of its own, one per run, and every one of those keeps its refresh loop until the process ends. So the CLI
// posts here and the room dials with the dialer it already has. The agent listener is loopback, never the board.

// HubDialer is the room's way to the hub, as far as this door goes.
type HubDialer interface {
	Dial(ctx context.Context) (net.Conn, error)
}

// hubDialUpgrade is the Upgrade token, mirrored in internal/cli.
const hubDialUpgrade = "atrium-hubdial"

type hubDialHolder struct {
	mu sync.Mutex
	d  HubDialer
}

// SetHubDialer wires the room's ladder up for commands on this machine. Called once the link is built.
func (d *Daemon) SetHubDialer(h HubDialer) {
	d.hubDial.mu.Lock()
	d.hubDial.d = h
	d.hubDial.mu.Unlock()
}

func (d *Daemon) hubDialer() HubDialer {
	d.hubDial.mu.Lock()
	defer d.hubDial.mu.Unlock()
	return d.hubDial.d
}

// handleHubDial is `POST /hubdial`, upgraded to the raw connection the room's ladder dialled to the hub.
func (d *Daemon) handleHubDial(w http.ResponseWriter, r *http.Request) {
	refuse := func(status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	if !loopbackPeer(r) {
		refuse(http.StatusForbidden, "the hub is dialled from this machine only")
		return
	}
	if r.Method != http.MethodPost || !strings.EqualFold(r.Header.Get("Upgrade"), hubDialUpgrade) ||
		!strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		refuse(http.StatusBadRequest, "that is not a hub dial request")
		return
	}
	h := d.hubDialer()
	if h == nil {
		refuse(http.StatusServiceUnavailable, "this room is not attached to a hub right now")
		return
	}
	up, err := h.Dial(r.Context())
	if err != nil {
		refuse(http.StatusBadGateway, err.Error())
		return
	}
	hj, ok := w.(http.Hijacker)
	if !ok {
		up.Close()
		refuse(http.StatusInternalServerError, "this connection cannot be taken over")
		return
	}
	c, rw, err := hj.Hijack()
	if err != nil {
		up.Close()
		return
	}
	_ = c.SetDeadline(time.Time{})
	if _, err := io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+hubDialUpgrade+"\r\n\r\n"); err != nil {
		c.Close()
		up.Close()
		return
	}
	joinStreams(&readerConn{Conn: c, br: rw.Reader}, up)
}
