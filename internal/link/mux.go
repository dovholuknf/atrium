package link

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/hashicorp/yamux"
)

// One mutual TLS connection per room, every link connection a stream on it.
//
// A room on direct used to open a TCP connection and a TLS handshake for every Dial: the control link, each warm data
// connection, every relay, git fetch and announcement. The overlays already ride one session per process (a ziti
// context, or the zrok environment's), and this makes direct do the same. See docs/fabric/one-session-per-room.md.
//
// ── the gate ─────────────────────────────────────────────
//
// The first line on a muxed connection is a hello of kind `mux`, after the TLS handshake and before anything else. A
// hub that knows it answers OK and runs yamux on the rest of the connection, and every stream it accepts is handed to
// the hub's accept loop as if it were a connection of its own: it starts with its own hello and is read by the code
// that always read one. A hub that does not know the kind refuses it, as it refuses any kind it has not heard of, and
// the room dials one connection per Dial as it always did, asking again after muxRetry. A room on an old build never
// says `mux`, and a new hub serves its connections as before. So rooms and hubs upgrade in any order.
//
// ── who the room is ──────────────────────────────────────
//
// Each stream carries the TLS state of the connection under it, so `peerCert` reads the same hub-signed certificate it
// read when every stream was a connection. A connection without a client certificate is refused the mux, as it is
// refused control and data.

const muxKind = "mux"

// muxRetry is how long a room waits before asking a hub that refused the mux again. A hub is upgraded at its own pace,
// and one refused hello in this long is nothing.
const muxRetry = 10 * time.Minute

// muxStreams caps the streams one room may hold open on the hub. The room's data dials are capped at maxDataDials, so
// this only bites a room that misbehaves, and it bounds the hub's memory at this many windows per room.
const muxStreams = 256

func muxConfig() *yamux.Config {
	c := yamux.DefaultConfig()
	// The beat on the control stream decides the link is gone. This ends a session nothing is beating on, such as
	// one only the path probe uses, so the next dial opens a fresh one.
	c.KeepAliveInterval = 15 * time.Second
	// A write that cannot be queued this long ends the session. Equal to Silence, so the two give up together.
	c.ConnectionWriteTimeout = DefaultTimings().Silence
	// The window stays at yamux's 256 KiB: a control frame can wait behind every bulk stream's window, and a bigger
	// one is a beat that misses Silence on a slow uplink.
	c.StreamCloseTimeout = time.Minute
	c.LogOutput = log.Writer()
	return c
}

// muxStream is one stream, carrying the TLS state of the connection it rides on.
type muxStream struct {
	net.Conn
	tc   *tls.Conn
	done func()
}

func (s *muxStream) ConnectionState() tls.ConnectionState { return s.tc.ConnectionState() }

func (s *muxStream) Close() error {
	if s.done != nil {
		s.done()
	}
	return s.Conn.Close()
}

// onMuxStream says whether a connection is a stream inside a room's session.
func onMuxStream(c net.Conn) bool {
	_, ok := c.(*muxStream)
	return ok
}

// tlsPeeked is a TLS connection whose first line has been looked at and not consumed.
type tlsPeeked struct {
	*peeked
	tc *tls.Conn
}

func (p tlsPeeked) ConnectionState() tls.ConnectionState { return p.tc.ConnectionState() }

// ── the hub's side ───────────────────────────────────────

// muxListener hands on every connection that did not ask for the mux as it arrived, and every stream of one that did.
type muxListener struct {
	net.Listener
	out  chan accepted
	done chan struct{}
	once sync.Once

	mu    sync.Mutex
	sess  map[*yamux.Session]struct{}
	conns int
}

func newMuxListener(ln net.Listener) *muxListener {
	m := &muxListener{Listener: ln, out: make(chan accepted), done: make(chan struct{}),
		sess: map[*yamux.Session]struct{}{}}
	go m.loop()
	return m
}

func (m *muxListener) loop() {
	for {
		c, err := m.Listener.Accept()
		if err != nil {
			select {
			case m.out <- accepted{err: err}:
			case <-m.done:
			}
			return
		}
		go m.sort(c)
	}
}

func (m *muxListener) hand(c net.Conn) bool {
	select {
	case m.out <- accepted{conn: c}:
		return true
	case <-m.done:
		_ = c.Close()
		return false
	}
}

// sort reads the first line without consuming it, in its own goroutine so one caller that says nothing cannot stall
// the accept loop.
func (m *muxListener) sort(c net.Conn) {
	tc, ok := c.(*tls.Conn)
	if !ok {
		m.hand(c)
		return
	}
	_ = c.SetDeadline(time.Now().Add(handshakeWait))
	p := &peeked{Conn: c, r: bufio.NewReaderSize(c, 4096)}
	line, err := peekLine(p.r)
	_ = c.SetDeadline(time.Time{})
	if err != nil {
		_ = c.Close()
		return
	}
	h := decodeHello(line)
	if h.Kind != muxKind {
		m.hand(tlsPeeked{peeked: p, tc: tc})
		return
	}
	_, _ = p.r.Discard(len(line))
	if h.V != Version || !DirectAuthenticated(tc) {
		_ = writeJSON(c, welcome{OK: false, Error: "a muxed link needs this link version and a room certificate"})
		_ = c.Close()
		return
	}
	if err := writeJSON(c, welcome{OK: true}); err != nil {
		_ = c.Close()
		return
	}
	s, err := yamux.Server(p, muxConfig())
	if err != nil {
		_ = c.Close()
		return
	}
	m.track(s, true)
	defer m.track(s, false)
	go func() {
		select {
		case <-m.done:
			_ = s.Close()
		case <-s.CloseChan():
		}
	}()
	var open atomic.Int32
	for {
		st, err := s.Accept()
		if err != nil {
			_ = s.Close()
			return
		}
		if open.Add(1) > muxStreams {
			open.Add(-1)
			log.Printf("[hub] a room holds %d streams on one link, closing the next", muxStreams)
			_ = st.Close()
			continue
		}
		var once sync.Once
		ms := &muxStream{Conn: st, tc: tc, done: func() { once.Do(func() { open.Add(-1) }) }}
		if !m.hand(ms) {
			return
		}
	}
}

func (m *muxListener) track(s *yamux.Session, on bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if on {
		m.sess[s] = struct{}{}
	} else {
		delete(m.sess, s)
	}
}

// Sessions is how many muxed room connections this listener holds open.
func (m *muxListener) Sessions() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.sess)
}

func (m *muxListener) Accept() (net.Conn, error) {
	select {
	case a := <-m.out:
		return a.conn, a.err
	case <-m.done:
		return nil, net.ErrClosed
	}
}

func (m *muxListener) Close() error {
	m.once.Do(func() { close(m.done) })
	return m.Listener.Close()
}

// peekLine looks at the first line without consuming it. A first line that does not fit the buffer is no hello this
// listener cares about, and goes on unread to the code that reads it bounded.
func peekLine(r *bufio.Reader) ([]byte, error) {
	n := 1
	for {
		if _, err := r.Peek(n); err != nil {
			return nil, err
		}
		b, _ := r.Peek(r.Buffered())
		if i := bytes.IndexByte(b, '\n'); i >= 0 {
			return b[:i+1], nil
		}
		if n >= r.Size() {
			return nil, nil
		}
		n = max(r.Buffered(), n) + 1
	}
}

func decodeHello(line []byte) hello {
	var h hello
	_ = json.Unmarshal(bytes.TrimSpace(line), &h)
	return h
}

// ── the room's side ──────────────────────────────────────

// muxClient is a room's one session to one hub address.
type muxClient struct {
	// sem is held while the session is being opened, so concurrent dials wait for one session rather than open several.
	sem chan struct{}

	mu      sync.Mutex
	sess    *yamux.Session
	refused time.Time
	opened  int
}

var (
	muxMu      sync.Mutex
	muxClients = map[string]*muxClient{}
)

// muxFor is the room's session to one hub address on one lane: control, or everything else.
func muxFor(d Direct, control bool) *muxClient {
	key := d.Addr + "|" + d.Keys.Dir
	if control {
		key += "|control"
	}
	muxMu.Lock()
	defer muxMu.Unlock()
	c, ok := muxClients[key]
	if !ok {
		c = &muxClient{sem: make(chan struct{}, 1)}
		muxClients[key] = c
	}
	return c
}

// live is the session when it is up.
func (c *muxClient) live() *yamux.Session {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.sess != nil && !c.sess.IsClosed() {
		return c.sess
	}
	return nil
}

func (c *muxClient) refusedRecently() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.refused.IsZero() && time.Since(c.refused) < muxRetry
}

// errMuxRefused is a hub that does not know the mux. The caller dials the old way.
var errMuxRefused = errors.New("the hub does not take a muxed link")

// session returns the live session, opening one when there is none.
func (c *muxClient) session(ctx context.Context, d Direct) (*yamux.Session, error) {
	if s := c.live(); s != nil {
		return s, nil
	}
	select {
	case c.sem <- struct{}{}:
	case <-ctx.Done():
		return nil, ctx.Err()
	}
	defer func() { <-c.sem }()
	if s := c.live(); s != nil {
		return s, nil
	}
	if c.refusedRecently() {
		return nil, errMuxRefused
	}
	conn, err := d.dialTLS(ctx)
	if err != nil {
		return nil, err
	}
	br := bufio.NewReader(conn)
	var w welcome
	err = conn.SetDeadline(time.Now().Add(handshakeWait))
	if err == nil {
		err = writeJSON(conn, hello{V: Version, Kind: muxKind})
	}
	if err == nil {
		err = readJSON(br, &w)
	}
	if err == nil {
		err = conn.SetDeadline(time.Time{})
	}
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if !w.OK {
		_ = conn.Close()
		c.mu.Lock()
		c.refused = time.Now()
		c.mu.Unlock()
		log.Printf("[room] %s does not take a muxed link (%s). one connection per dial until it does", d.Addr, w.Error)
		return nil, errMuxRefused
	}
	s, err := yamux.Client(&peeked{Conn: conn, r: br}, muxConfig())
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	c.mu.Lock()
	c.sess = s
	c.refused = time.Time{}
	c.opened++
	c.mu.Unlock()
	return s, nil
}

// open is one stream on the session. A session that died since it was handed out is reopened once.
func (c *muxClient) open(ctx context.Context, d Direct) (net.Conn, error) {
	for try := 0; ; try++ {
		s, err := c.session(ctx, d)
		if err != nil {
			return nil, err
		}
		st, err := s.Open()
		if err == nil {
			return st, nil
		}
		if try > 0 || !s.IsClosed() {
			return nil, fmt.Errorf("could not open a stream to the hub: %w", err)
		}
	}
}

// Opened is how many sessions this client has opened in its life.
func (c *muxClient) Opened() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.opened
}

// close ends the session, so the next dial opens another.
func (c *muxClient) close() {
	c.mu.Lock()
	s := c.sess
	c.sess = nil
	c.mu.Unlock()
	if s != nil {
		_ = s.Close()
	}
}
