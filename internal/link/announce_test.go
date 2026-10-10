package link

import (
	"encoding/json"
	"net"
	"sync"
)

// A room telling its hub what it is holding.
//
// Over a real listener with a real hub and a real room, for the same reason the
// rest of this package's tests are: the bugs worth catching live in the
// handover between a framed handshake and whatever follows it, and the first
// one found here was exactly that. The hub read the body before answering the
// hello and the room waited for that answer before sending the body, so both
// sides sat there until a deadline fired. Neither half is wrong on its own.

type sniffing struct {
	net.Conn
	saw  func(kind string)
	once sync.Once
}

func (s *sniffing) Write(p []byte) (int, error) {
	s.once.Do(func() {
		var h hello
		if err := json.Unmarshal(trimLine(p), &h); err == nil && s.saw != nil {
			s.saw(h.Kind)
		}
	})
	return s.Conn.Write(p)
}

func trimLine(p []byte) []byte {
	for i, b := range p {
		if b == '\n' {
			return p[:i]
		}
	}
	return p
}
