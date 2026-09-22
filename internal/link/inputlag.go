package link

import (
	"bytes"
	"net"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
)

// lagConn times the hub's hop of a terminal attach, and is only ever built
// when ATRIUM_DEBUG_INPUTLAG is set. See internal/inputlag.
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

// lagWrap wraps a dialled room connection when the logging is on, and returns
// it untouched when it is not.
func lagWrap(c net.Conn, room string) net.Conn {
	if !inputlag.On() || c == nil {
		return c
	}
	return &lagConn{Conn: c, room: room}
}

func (c *lagConn) Write(b []byte) (int, error) {
	if !c.ws.Load() {
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
	if at := c.up.Swap(0); at != 0 {
		if d := time.Since(time.Unix(0, at)); inputlag.Over(d) {
			inputlag.Logf("hub %s echo: frame up -> first bytes back %s", c.room, inputlag.Ms(d))
		}
	}
	return n, err
}
