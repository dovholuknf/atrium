package link

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// The hub's half: accept what rooms dial, and lend those connections out as if
// the hub had dialled them.
//
// THE HUB HOLDS NOTHING DURABLE. No database, no card, no scrollback. Kill it
// mid-sentence and the only thing lost is the socket. That is not tidiness, it
// is the entire point: the hub is the part being restarted every few minutes
// while somebody changes CSS, so it must never be the part that owns anything
// worth keeping.

// Hub accepts rooms and proxies to them.
type Hub struct {
	T Timings
	// Enrol answers a room that is joining for the first time and has no
	// certificate yet. Supplied by the transport, because what a credential IS
	// is the transport's business and not this file's. Nil refuses enrolment,
	// which is correct for a transport where identity comes from elsewhere:
	// an OpenZiti service has already decided who may connect before a byte
	// arrives here, so there is nothing to enrol.
	Enrol func(net.Conn, *bufio.Reader) (string, error)
	// Authenticated reports whether a connection proved who it is. A transport
	// that carries identity itself answers true. Nil means "yes", which is
	// right for a test over a pipe and nowhere else.
	Authenticated func(net.Conn) bool

	mu    sync.Mutex
	rooms map[string]*attached
}

// attached is one room's live link.
type attached struct {
	name    string
	version string
	host    string
	session string
	since   time.Time
	// key identifies the credential this room attached with, so a reconnect
	// can be told from somebody else claiming the same name. Empty on a
	// transport that carries no key of its own, where the network has already
	// decided and there is nothing here to defend.
	key string

	// control is the connection the room dialled first, and the only one that
	// stays framed. Writing to it asks for more data connections.
	control net.Conn
	// idle holds data connections nobody is using. Buffered generously: a room
	// answering a burst of `need` all at once must not block on handing them
	// over.
	idle chan net.Conn
	// want is how many spare connections to keep. Grows with load.
	want int

	lastBeat time.Time
	mu       sync.Mutex
	closed   bool
	done     chan struct{}
}

// NewHub makes an empty hub.
func NewHub(t Timings) *Hub {
	return &Hub{T: t.fill(), rooms: map[string]*attached{}}
}

// Serve accepts connections until the listener closes.
//
// The listener is whatever the transport handed over: a TLS listener today, a
// ziti or zrok one later. This function does not know and must not learn.
func (h *Hub) Serve(ctx context.Context, ln net.Listener) error {
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go h.take(ctx, conn)
	}
}

// take handles one freshly accepted connection, whichever kind it is.
func (h *Hub) take(ctx context.Context, conn net.Conn) {
	br := bufio.NewReader(conn)
	hi, err := hearHello(conn, br)
	if err != nil {
		log.Printf("[hub] refused a connection: %v", err)
		conn.Close()
		return
	}

	// ENROLMENT IS THE ONE KIND THAT MAY ARRIVE UNAUTHENTICATED, because a room
	// that has never joined has nothing to authenticate with. It proves itself
	// with the one-time secret instead, and leaves with a credential.
	if hi.Kind == "enrol" {
		defer conn.Close()
		if h.Enrol == nil {
			_ = writeJSON(conn, welcome{OK: false,
				Error: "this hub does not enrol rooms. who may connect is decided by its transport"})
			return
		}
		if err := writeJSON(conn, welcome{OK: true}); err != nil {
			return
		}
		who, err := h.Enrol(conn, br)
		if err != nil {
			log.Printf("[hub] a room could not join: %v", err)
			return
		}
		log.Printf("[hub] room %q joined and has a certificate", who)
		return
	}

	// EVERY OTHER KIND MUST HAVE PROVED ITSELF. Checked before the name is
	// read, so an unauthenticated connection cannot even claim one.
	if h.Authenticated != nil && !h.Authenticated(conn) {
		_ = writeJSON(conn, welcome{OK: false,
			Error: "this connection presented no credential. run `atrium2 join` first"})
		conn.Close()
		return
	}

	// WHO THIS IS COMES FROM THE CERTIFICATE, NOT THE FRAME.
	//
	// The hello carries a name because a room should be able to label itself,
	// and a label is not an identity. `identify` asks the transport who
	// actually authenticated, and the answer wins. A transport with no identity
	// to offer (a plain listener in a test) falls back to the claim, which is
	// correct there and nowhere else.
	name := hi.Room
	if who := identify(conn); who != "" {
		if name != "" && !equalFold(who, name) {
			log.Printf("[hub] %q presented a certificate for %q, using the certificate", name, who)
		}
		name = who
	}
	if name == "" {
		_ = writeJSON(conn, welcome{OK: false, Error: "this room has no name"})
		conn.Close()
		return
	}

	switch hi.Kind {
	case "control":
		h.control(ctx, name, hi, conn, br)
	case "data":
		h.data(name, hi, conn, br)
	}
}

// control adopts a room and holds its control connection.
func (h *Hub) control(ctx context.Context, name string, hi hello, conn net.Conn, br *bufio.Reader) {
	session := newSession()
	a := &attached{
		name: name, version: hi.Version, host: hi.Host, session: session,
		since: time.Now(), control: conn,
		// Room for a burst plus the terminals a board is likely to hold open.
		idle:     make(chan net.Conn, 64),
		want:     h.T.Warm,
		lastBeat: time.Now(),
		done:     make(chan struct{}),
	}

	// A ROOM RECONNECTING REPLACES ITSELF, and the old one is closed rather
	// than left. A hub restart looks identical to a network blip from the
	// room's side, so the room redials while the hub may still be holding a
	// half-open version of the previous link. Keeping both would mean requests
	// going down a socket nothing is reading.
	//
	// BUT ONLY THE SAME KEY MAY DO IT, and that check is the difference
	// between a reconnect and a takeover.
	//
	// A join secret authorises a name of the caller's choosing, so somebody
	// holding one could enrol under the name of a room that is already
	// attached, dial, and have the hub evict the real room and serve theirs to
	// the browser instead. Comparing the certificate's public key means a
	// reconnect is the same room proving it is the same room, and a different
	// key under a taken name is refused rather than obeyed.
	a.key = peerKey(conn)
	h.mu.Lock()
	old, taken := h.rooms[keyOf(name)]
	if taken && old.key != "" && a.key != "" && old.key != a.key {
		h.mu.Unlock()
		log.Printf("[hub] refused a second %q: a different certificate is already attached", name)
		_ = writeJSON(conn, welcome{OK: false, Error: "a different room is already attached " +
			"under that name. pick another name, or stop the one that is there"})
		conn.Close()
		return
	}
	if taken {
		old.close("replaced by a newer connection from the same room")
	}
	h.rooms[keyOf(name)] = a
	h.mu.Unlock()

	log.Printf("[hub] room %q attached from %s", name, conn.RemoteAddr())
	if err := writeJSON(conn, welcome{OK: true, Session: session, Warm: h.T.Warm}); err != nil {
		a.close(err.Error())
		return
	}

	go h.watch(ctx, a)

	for {
		var n note
		if err := readJSON(br, &n); err != nil {
			break
		}
		if n.Beat > 0 {
			a.mu.Lock()
			a.lastBeat = time.Now()
			a.mu.Unlock()
			// Echoed, so the room can tell a live socket from a half-open one.
			// A write that succeeds into a dead connection is the failure mode
			// a heartbeat exists to catch, and only a round trip catches it.
			_ = writeJSON(conn, note{Beat: n.Beat})
		}
	}

	a.close("the room hung up")
	h.forget(name, a)
}

// watch evicts a room that has stopped beating.
func (h *Hub) watch(ctx context.Context, a *attached) {
	t := time.NewTicker(h.T.Beat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-a.done:
			return
		case <-t.C:
			a.mu.Lock()
			quiet := time.Since(a.lastBeat)
			a.mu.Unlock()
			if quiet > h.T.Silence {
				log.Printf("[hub] room %q went quiet for %s", a.name, quiet.Round(time.Second))
				a.close("no heartbeat")
				h.forget(a.name, a)
				return
			}
		}
	}
}

// data files a fresh connection into a room's pool.
func (h *Hub) data(name string, hi hello, conn net.Conn, br *bufio.Reader) {
	h.mu.Lock()
	a := h.rooms[keyOf(name)]
	h.mu.Unlock()

	// A DATA CONNECTION WITHOUT ITS CONTROL CONNECTION IS REFUSED, and the
	// session is why. Two rooms enrolling at once, or one room reconnecting
	// while its old data connections are still in flight, would otherwise
	// cross their pools and serve one room's board out of another's process.
	if a == nil || hi.Session == "" || hi.Session != a.session {
		_ = writeJSON(conn, welcome{OK: false, Error: "that session is not current. reconnect"})
		conn.Close()
		return
	}
	if err := writeJSON(conn, welcome{OK: true}); err != nil {
		conn.Close()
		return
	}
	select {
	case a.idle <- &buffered{Conn: conn, br: br}:
	default:
		// More than the pool wants. Closed rather than queued: a connection
		// nobody will use is a file handle on both machines.
		conn.Close()
	}
}

func (h *Hub) forget(name string, a *attached) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.rooms[keyOf(name)]; ok && cur == a {
		delete(h.rooms, keyOf(name))
	}
}

// close tears a room down once.
func (a *attached) close(why string) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.closed = true
	a.mu.Unlock()

	_ = writeJSON(a.control, note{Bye: why})
	_ = a.control.Close()
	close(a.done)
	// Every pooled connection goes too. A request already in flight on one is
	// killed, which is correct: the room it was talking to is gone, and a
	// board that hangs is worse than one that says so.
	for {
		select {
		case c := <-a.idle:
			c.Close()
		default:
			return
		}
	}
}

// ── lending connections out ─────────────────────────────

// ErrNoRoom is what a request for an unattached room comes back as.
var ErrNoRoom = errors.New("no room is attached")

// Dial hands out a connection to a room, for the proxy's transport.
//
// The signature is `http.Transport.DialContext`, which is the whole reason this
// works: Go's own client machinery does the keep-alive, the pipelining refusal,
// the 100-continue and the upgrade handling, and it never finds out that the
// connection was dialled from the other end.
func (h *Hub) Dial(ctx context.Context, room string) (net.Conn, error) {
	h.mu.Lock()
	a := h.rooms[keyOf(room)]
	h.mu.Unlock()
	if a == nil {
		return nil, ErrNoRoom
	}

	// Ask for a replacement BEFORE waiting, not after. Asking afterwards means
	// every request under load pays the dial latency it was supposed to avoid.
	//
	// Only when the pool is actually short, though. Asking on every request
	// made the room dial a fresh connection per board request under load, most
	// of which arrived to a full pool and were closed on sight: a whole
	// handshake per request, for nothing.
	if len(a.idle) < a.want {
		a.request(1)
	}

	select {
	case c := <-a.idle:
		return c, nil
	case <-a.done:
		return nil, ErrNoRoom
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(h.T.DialWait):
		// A room that is attached and not answering. Bounded, because the
		// alternative is a board that spins rather than a board that says what
		// is wrong.
		return nil, errors.New("the room is attached but did not answer in time")
	}
}

// request asks a room for more data connections.
func (a *attached) request(n int) {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return
	}
	a.mu.Unlock()
	// Best effort. A failed write means the control connection is gone, which
	// the reader will notice, and there is nothing useful to do here about it.
	_ = writeJSON(a.control, note{Need: n})
}

// dialer is `Dial` bound to one room, in the shape `http.Transport` wants.
func (h *Hub) dialer(room string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return h.Dial(ctx, room)
	}
}

// Room reports what is attached under a name.
type Attached struct {
	Name    string    `json:"name"`
	Version string    `json:"version,omitempty"`
	Host    string    `json:"host,omitempty"`
	Since   time.Time `json:"since"`
	Idle    int       `json:"idle"`
	Beat    time.Time `json:"last_beat"`
}

// Rooms lists what is attached, for the hub's own status endpoint.
func (h *Hub) Rooms() []Attached {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]Attached, 0, len(h.rooms))
	for _, a := range h.rooms {
		a.mu.Lock()
		beat := a.lastBeat
		a.mu.Unlock()
		out = append(out, Attached{
			Name: a.name, Version: a.version, Host: a.host,
			Since: a.since, Idle: len(a.idle), Beat: beat,
		})
	}
	return out
}

// Has reports whether a room is attached right now.
func (h *Hub) Has(room string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.rooms[keyOf(room)]
	return ok
}

// Only returns the single attached room's name when there is exactly one.
//
// THE WHOLE POINT OF THE FIRST RELEASE. One hub, one room, and a board that
// needs no room picker because there is nothing to pick. When a second room
// attaches this answers empty and the caller has to decide, which is where the
// board grows a control and not before.
func (h *Hub) Only() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.rooms) != 1 {
		return ""
	}
	for _, a := range h.rooms {
		return a.name
	}
	return ""
}

// peerKey fingerprints the credential a connection presented, so a reconnect
// can be distinguished from an impersonation. Set by the transport, for the
// same reason `identify` is.
var peerKey = func(net.Conn) string { return "" }

// identify asks the transport who authenticated, as a name.
//
// Set by whichever transport file is compiled in. Direct mTLS reads the client
// certificate's common name. A transport with no identity leaves it nil and the
// hello's claim is used, which is right for a test over a pipe and wrong
// everywhere else.
var identify = func(net.Conn) string { return "" }

func newSession() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// keyOf folds a room name, because a room typed with different capitals on two
// machines is one room.
func keyOf(s string) string { return lowerASCII(s) }

func equalFold(a, b string) bool { return lowerASCII(a) == lowerASCII(b) }

// lowerASCII rather than strings.ToLower, so a room name is folded the same way
// on every machine regardless of locale. A Turkish dotless i is a real thing
// and a room name is not prose.
func lowerASCII(s string) string {
	out := []byte(s)
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}

var _ = http.StatusOK
