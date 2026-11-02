package hublink

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// An ssh stream, from a person on this machine to another room's sshd, through the hub. See docs/rnd/ssh-via-hub.md.
//
// THE ROOM HOLDS THE TRANSPORT, so `atrium ssh-proxy` posts here and the room dials the hub with the dialer it already
// has, ziti identity loaded. A CLI loading its own per ssh connection would cost seconds per scp. The agent listener is
// loopback, the door every agent command already uses. Atrium carries bytes and nothing else on this route.

// Streamer is the room's link, as far as streams go. Whoever builds the daemon supplies it, since this package does not
// import the link.
type Streamer interface {
	// OpenStream asks the hub for a stream to target's sshd. The returned connection carries ssh's bytes. An error is
	// the sentence a person should read.
	OpenStream(ctx context.Context, target, claim string) (net.Conn, error)
	// OpenStreams is how many streams this room is carrying for others.
	OpenStreams() int
}

// streamUpgrade is the Upgrade token. Mirrored from internal/link, which this package does not import.
const StreamUpgrade = "atrium-stream"

type StreamHolder struct {
	Mu sync.Mutex
	S  Streamer
}

// SetStreamer wires the room's link up for ssh streams. Called once the link is built.
func (d *Service) SetStreamer(s Streamer) {
	d.Streams.Mu.Lock()
	d.Streams.S = s
	d.Streams.Mu.Unlock()
}

func (d *Service) Streamer() Streamer {
	d.Streams.Mu.Lock()
	defer d.Streams.Mu.Unlock()
	return d.Streams.S
}

// handleStreamStatus is `GET /stream/status`: how many streams this room carries, for `atrium room remote-access`.
func (d *Service) HandleStreamStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	n := 0
	if s := d.Streamer(); s != nil {
		n = s.OpenStreams()
	}
	_ = json.NewEncoder(w).Encode(map[string]int{"open": n})
}

// handleStream is `POST /stream`, `{"room": ...}` in the X-Atrium-Room header, upgraded to raw bytes.
func (d *Service) HandleStream(w http.ResponseWriter, r *http.Request) {
	refuse := func(status int, msg string) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		fmt.Fprintf(w, `{"error":%q}`, msg)
	}
	if !LoopbackPeer(r) {
		refuse(http.StatusForbidden, "ssh streams are opened from this machine only")
		return
	}
	if r.Method != http.MethodPost || !strings.EqualFold(r.Header.Get("Upgrade"), StreamUpgrade) ||
		!strings.Contains(strings.ToLower(r.Header.Get("Connection")), "upgrade") {
		refuse(http.StatusBadRequest, "that is not a stream request")
		return
	}
	target := strings.TrimSpace(r.Header.Get("X-Atrium-Room"))
	if target == "" {
		refuse(http.StatusBadRequest, "name the room to reach")
		return
	}
	s := d.Streamer()
	if s == nil {
		refuse(http.StatusServiceUnavailable, "this room is not attached to a hub right now")
		return
	}
	up, err := s.OpenStream(r.Context(), target, r.Header.Get("X-Atrium-Claim"))
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
	if _, err := io.WriteString(c, "HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: "+StreamUpgrade+"\r\n\r\n"); err != nil {
		c.Close()
		up.Close()
		return
	}
	// The hub counts and audits. This hop only joins two connections, with half close, and the hub's own linger bound
	// ends a stream whose other half never finishes.
	JoinStreams(&ReaderConn{Conn: c, Br: rw.Reader}, up)
}

func LoopbackPeer(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

type ReaderConn struct {
	net.Conn
	Br *bufio.Reader
}

func (c *ReaderConn) Read(p []byte) (int, error) { return c.Br.Read(p) }

func (c *ReaderConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

// joinStreams copies both ways, half closing as each side finishes, and returns when both are done. Any error closes
// both at once.
func JoinStreams(a, b net.Conn) {
	done := make(chan struct{}, 2)
	cp := func(dst, src net.Conn) {
		_, err := io.CopyBuffer(dst, src, make([]byte, 32<<10))
		if err != nil {
			_ = a.Close()
			_ = b.Close()
		} else if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		}
		done <- struct{}{}
	}
	go cp(b, a)
	go cp(a, b)
	<-done
	// One direction is over. The other gets the hub's linger, and the hub closes both when it ends.
	select {
	case <-done:
	case <-time.After(35 * time.Second):
	}
	_ = a.Close()
	_ = b.Close()
}
