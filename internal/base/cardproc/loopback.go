package cardproc

import (
	"fmt"
	"net"
)

// Previews is the block a room lets its hub forward a browser to. No port atrium has the system pick is in it, so a
// preview never reaches an atrium listener, which trusts loopback.
var Previews = Range{Lo: 50000, Hi: 50999}

// ListenLoopback listens on a 127.0.0.1 port the system picks, never one in Previews.
func ListenLoopback() (net.Listener, error) {
	return listenOutside(Previews, func() (net.Listener, error) { return net.Listen("tcp", "127.0.0.1:0") })
}

// listenOutside listens until a port outside r comes back, and fails closed after a bound. The bound clears the whole
// block, because Windows hands out ports in sequence and a pick inside the block can be followed by the rest of it.
func listenOutside(r Range, listen func() (net.Listener, error)) (net.Listener, error) {
	var held []net.Listener
	defer func() {
		for _, l := range held {
			_ = l.Close()
		}
	}()
	bound := r.Hi - r.Lo + 1 + 64
	for i := 0; i < bound; i++ {
		ln, err := listen()
		if err != nil {
			return nil, err
		}
		if !r.Has(ln.Addr().(*net.TCPAddr).Port) {
			return ln, nil
		}
		// Held, not closed, so the system cannot hand the same port straight back.
		held = append(held, ln)
	}
	return nil, fmt.Errorf("the system picked %d ports in a row inside %s", bound, r)
}
