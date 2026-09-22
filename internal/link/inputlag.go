package link

import (
	"bytes"
	"net"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// lagConn times the hub's hop of a terminal attach when input-lag logging is on.
// See internal/inputlag.
//
// BUILT FOR EVERY ROOM CONNECTION, on or off. The logging is switched live from
// the gear, and an attach dialled while it was off would otherwise stay untimed
// until somebody reattached, which is the terminal they were typing into when
// they asked. Off, a frame costs one atomic load. The 101 is still watched for
// while off, so a switch mid-attach times that attach from the next keystroke.
//
// THE PROXY NEVER SEES A FRAME. An attach is an upgrade, and after the 101
// `httputil.ReverseProxy` copies raw bytes both ways between two hijacked
// connections. So this sits on the room side of that copy, where the bytes
// still pass through a Read and a Write this package owns:
//
//	up    a Write toward the room, which is the browser's keystroke frame
//	echo  that Write -> the first bytes read back from the room
//
// Compared with the room's own "echo" line for the same moment, the difference
// is the link between hub and room. Compared with the browser's round trip, the
// difference is the browser-to-hub leg plus the board itself.
//
// Silent until the connection upgrades, because the same pooled connections
// carry plain requests, and an event stream's "echo" is an hour long.
type lagConn struct {
	net.Conn
	room string
	ws   atomic.Bool
	up   atomic.Int64
}

var switching = []byte("HTTP/1.1 101")

// lagWrap wraps a dialled room connection.
func lagWrap(c net.Conn, room string) net.Conn {
	if c == nil {
		return c
	}
	return &lagConn{Conn: c, room: room}
}

func (c *lagConn) Write(b []byte) (int, error) {
	if !c.ws.Load() || !inputlag.On() {
		// Dropped rather than left, so an echo clock started before a switch
		// off does not close as one enormous hop after the switch back on.
		c.up.Store(0)
		return c.Conn.Write(b)
	}
	t0 := time.Now()
	n, err := c.Conn.Write(b)
	if d := time.Since(t0); inputlag.Over(d) {
		inputlag.Logf("hub %s up: write toward the room took %s (%d bytes)", c.room, inputlag.Ms(d), n)
	}
	c.up.CompareAndSwap(0, t0.UnixNano())
	return n, err
}

func (c *lagConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n <= 0 {
		return n, err
	}
	if !c.ws.Load() {
		// An upgraded connection never goes back to the pool, so once is enough.
		if bytes.HasPrefix(b[:n], switching) {
			c.ws.Store(true)
		}
		return n, err
	}
	if !inputlag.On() {
		return n, err
	}
	if at := c.up.Swap(0); at != 0 {
		if d := time.Since(time.Unix(0, at)); inputlag.Over(d) {
			inputlag.Logf("hub %s echo: frame up -> first bytes back %s", c.room, inputlag.Ms(d))
		}
	}
	return n, err
}
