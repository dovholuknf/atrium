package conn

import (
	"bufio"
	"net"
)

// Names the link package reaches for, because it serves the kinds and routes this package carries.
const (
	AnnounceMax    = announceMax
	ControlTimeout = controlTimeout
)

var (
	NewHub    = newHub
	HeaderHas = headerHas
	PathClip  = pathClip
)

// Buffered is a connection whose reader has already read ahead of the socket.
func Buffered(c net.Conn, br *bufio.Reader) net.Conn { return &buffered{Conn: c, br: br} }

// EndStreams stops taking streams and waits for every handler, for a test whose directories go when it ends.
func (r *Room) EndStreams() { r.endStreams() }
