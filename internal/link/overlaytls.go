package link

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"net"
	"sync"
	"time"
)

// The hub's certificate proves a room's name on an overlay too (decision 18).
//
// ── the rule, and why it is two halves ───────────────────
//
// An overlay decides who may REACH the hub. It cannot say which room is calling,
// so the room's name comes from the same hub-signed certificate direct uses, and
// the mutual TLS runs INSIDE the overlay. This file is that wrapper, for both
// sides: the hub wraps what its room-link listener accepts, and a room wraps what
// its transport dials.
//
// ── ALONGSIDE THE OLD PATH, NEVER INSTEAD OF IT ──────────
//
// A room that joined over ziti or zrok before this existed dials a plain
// connection and opens with a JSON hello. It must keep attaching, because a hub
// upgrade that turned every overlay room away would be an outage nobody chose.
// So the hub PEEKS at the first byte of each accepted connection. 0x16 is a TLS
// ClientHello and gets the direct transport's handshake. Anything else is the old
// path, authenticated as it always was, and marked as unproven so the operator
// can see who still has to re-join before switching it off.
//
// The peek reads a byte and gives it back. Nothing downstream can tell it happened.

// tlsHandshakeRecord is the first byte of every TLS ClientHello.
const tlsHandshakeRecord = 0x16

// peeked is a connection whose first byte has been looked at and not consumed.
type peeked struct {
	net.Conn
	r *bufio.Reader
}

func (p *peeked) Read(b []byte) (int, error) { return p.r.Read(b) }

// legacyConn is a connection that arrived WITHOUT TLS on an overlay listener.
//
// A distinct type rather than a flag on a shared one, because the hub asks
// "which kind is this" with a type assertion, exactly as `peerCert` does for the
// other kind, and a type cannot be forgotten to be set.
type legacyConn struct {
	*peeked
	transport string
}

// legacyTransport says whether a connection took the old, certificate-less path
// on an overlay listener, and which overlay it came in on.
func legacyTransport(c net.Conn) (string, bool) {
	l, ok := c.(*legacyConn)
	if !ok {
		return "", false
	}
	return l.transport, true
}

// OverlayAuthenticated is `Hub.Authenticated` for a room-link listener over an
// overlay. A certificate proves itself. The old path is the overlay's own word
// that the caller may reach the hub, which is all it ever was.
//
// Whether the old path is still ALLOWED is a different question, asked by
// `Hub.LegacyRefused` after this, so a connection without a certificate on the
// new path is refused here and one on the old path is refused with its own sentence.
func OverlayAuthenticated(c net.Conn) bool {
	if _, ok := legacyTransport(c); ok {
		return true
	}
	return DirectAuthenticated(c)
}

// RejoinSentence is what a room on the old path is told when the hub has switched
// it off. A sentence, because a closed socket sends somebody to a packet capture.
func RejoinSentence(room string) string {
	who := "this room"
	if room != "" {
		who = "the room " + room
	}
	return "this hub no longer accepts a room that cannot prove its name. run `atrium rooms token` " +
		"on the hub for " + who + ", and `atrium room join` here with the new join string"
}

// MixedListener wraps an overlay's listener so the hub can serve both kinds of
// room on it at once.
//
// Each accepted connection is peeked at in its own goroutine, bounded by the
// handshake deadline, so one caller that connects and says nothing cannot stall
// the accept loop for everyone behind it. `cfg` is what `Direct.ServerTLS`
// builds. `transport` names the overlay, for the audit line about an unproven room.
//
// ONLY THE ROOM-LINK LISTENER GETS THIS. The board is served over an overlay on a
// different listener, and a browser has no client certificate. Nothing here is
// reachable from the board's code, and a test pins that.
func MixedListener(ln net.Listener, cfg *tls.Config, transport string) net.Listener {
	m := &mixedListener{
		Listener: ln, cfg: cfg, transport: transport,
		out: make(chan accepted), done: make(chan struct{}),
	}
	go m.loop()
	return m
}

type accepted struct {
	conn net.Conn
	err  error
}

type mixedListener struct {
	net.Listener
	cfg       *tls.Config
	transport string
	out       chan accepted
	done      chan struct{}
	once      sync.Once
}

func (m *mixedListener) loop() {
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

// sort looks at the first byte and hands the connection on as the right kind.
func (m *mixedListener) sort(c net.Conn) {
	p := &peeked{Conn: c, r: bufio.NewReader(c)}
	_ = c.SetReadDeadline(time.Now().Add(handshakeWait))
	b, err := p.r.Peek(1)
	_ = c.SetReadDeadline(time.Time{})
	if err != nil {
		_ = c.Close()
		return
	}
	var out net.Conn
	if b[0] == tlsHandshakeRecord {
		out = tls.Server(p, m.cfg)
	} else {
		out = &legacyConn{peeked: p, transport: m.transport}
	}
	select {
	case m.out <- accepted{conn: out}:
	case <-m.done:
		_ = c.Close()
	}
}

func (m *mixedListener) Accept() (net.Conn, error) {
	select {
	case a := <-m.out:
		return a.conn, a.err
	case <-m.done:
		return nil, net.ErrClosed
	}
}

func (m *mixedListener) Close() error {
	m.once.Do(func() { close(m.done) })
	return m.Listener.Close()
}

// ── the room's side ──────────────────────────────────────

// Proven wraps a transport's Dialer so every connection it opens is mutual TLS
// with the room's certificate.
//
// IN THE DIALER, and not in each caller, so every kind of connection the room
// opens (control, data, enrol, upgrade, announce, relay, and the git kind that
// dials through `r.Dial.Dial`) is covered by construction. Nothing may open its
// own path to the hub.
type Proven struct {
	Dialer
	Keys Keys
}

// Dial opens one connection on the overlay and runs the handshake on it.
func (p Proven) Dial(ctx context.Context) (net.Conn, error) {
	cfg, err := Direct{Keys: p.Keys}.clientTLS()
	if err != nil {
		return nil, err
	}
	raw, err := p.Dialer.Dial(ctx)
	if err != nil {
		return nil, err
	}
	return handshakeClient(ctx, raw, cfg)
}

// handshakeClient runs the TLS handshake on a connection somebody else dialled,
// bounded by the caller's context and by the handshake deadline.
func handshakeClient(ctx context.Context, raw net.Conn, cfg *tls.Config) (net.Conn, error) {
	tc := tls.Client(raw, cfg)
	if err := raw.SetDeadline(time.Now().Add(handshakeWait)); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := tc.HandshakeContext(ctx); err != nil {
		_ = raw.Close()
		return nil, err
	}
	if err := raw.SetDeadline(time.Time{}); err != nil {
		_ = raw.Close()
		return nil, err
	}
	return tc, nil
}

// HasRoomCert says whether this room has enrolled and holds a certificate. A room
// with one dials an overlay inside TLS, and one without keeps the old path.
func (k Keys) HasRoomCert() bool {
	_, err := tls.LoadX509KeyPair(k.path("room.crt"), k.path("room.key"))
	return err == nil
}

// EnrolOver spends a join secret on a connection the transport dialled, then
// keeps the certificate that comes back. The overlay carries the bytes and the
// pinned handshake inside it proves the hub, exactly as over direct.
func EnrolOver(ctx context.Context, dial Dialer, keys Keys, pin, self, secret string) (string, error) {
	if pin == "" || secret == "" {
		return "", errors.New("this join string carries no secret to enrol with")
	}
	key, csr, err := NewCSR(self)
	if err != nil {
		return "", err
	}
	raw, err := dial.Dial(ctx)
	if err != nil {
		return "", err
	}
	conn, err := handshakeClient(ctx, raw, &tls.Config{
		MinVersion:            tls.VersionTLS13,
		InsecureSkipVerify:    true,
		VerifyPeerCertificate: pinnedTo(pin),
	})
	if err != nil {
		return "", err
	}
	return enrolOn(conn, keys, dial.Describe(), self, secret, key, csr)
}
