package cli

import (
	"log"
	"net"
)

// loopbackV6 listens on [::1] at the port of a 127.0.0.1 listener, or answers nil. A failure is logged and the board
// stays on 127.0.0.1 alone.
func loopbackV6(addr string) net.Listener {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || host != "127.0.0.1" {
		return nil
	}
	ln, err := net.Listen("tcp", net.JoinHostPort("::1", port))
	if err != nil {
		log.Printf("[hub] the board is not on [::1]:%s, so a browser asking for localhost may wait on it: %v", port, err)
		return nil
	}
	return ln
}
