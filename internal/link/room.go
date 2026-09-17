package link

import (
	"bufio"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"sync"
	"time"
)

// The room's half: dial the hub, and serve the board on what comes back.
//
// THE ROOM NEVER LEARNS A NEW PROTOCOL. Everything below exists to produce a
// net.Listener whose Accept returns dialled connections, so the last line of
// this file's job is the one the whole design is for:
//
//	http.Serve(listener, theSameHandlerItAlreadyServesOnLoopback)

// Room is a running link from a room to a hub.
type Room struct {
	// Name is what the operator called this room.
	Name string
	// Dial is the transport. Direct mTLS today, ziti or zrok later, and
	// nothing else in this file knows which.
	Dial Dialer
	// Handler is the board API. The SAME handler served on loopback, not a
	// copy and not a subset: a room with no hub is still a working atrium, and
	// that is the escape hatch when the hub is down.
	Handler http.Handler
	// Version and Host are shown on the hub. Observed, never authoritative.
	Version string
	Host    string
	// T is the timing set. Zero fields take defaults.
	T Timings

	// conns carries dialled connections to the listener's Accept. Buffered by
	// one so a dial that wins a race is not thrown away.
	conns chan net.Conn
	// session is what the hub called this connection's lifetime. Cleared on
	// every reconnect, since a new control connection is a new session and a
	// data connection carrying a stale one must be refused.
	mu      sync.Mutex
	session string
	up      bool
	since   time.Time
	lastErr string
}

// Run keeps a room attached to its hub until the context is cancelled.
//
// IT NEVER RETURNS AN ERROR FOR A HUB BEING DOWN, and that is the posture the
// whole daemon already takes for things it does not control. A hub that is not
// answering is a room that keeps working with nobody watching it, which is
// exactly what it was before anybody built a hub. So this logs, backs off and
// tries again, and the room's own board never notices.
func (r *Room) Run(ctx context.Context) error {
	r.T = r.T.fill()
	r.conns = make(chan net.Conn, 1)

	// The board server, on connections we dialled. Started once and left
	// running: it takes connections off the channel for the life of the room,
	// across any number of hub restarts, because an http.Server does not care
	// where its listener gets sockets from.
	srv := &http.Server{
		Handler: r.Handler,
		// EVERY TIMEOUT HERE IS ZERO, AND THAT IS THE WHOLE POINT OF THIS
		// COMMENT, because setting one looks obviously correct and breaks the
		// feature in a way that takes an evening to find.
		//
		// A `ReadHeaderTimeout` starts when the connection is ACCEPTED, not
		// when bytes arrive. These connections are dialled in advance and wait
		// in the hub's pool precisely so the first request after a hub restart
		// does not pay for a dial. So a pool that has been warm for longer than
		// the timeout is a pool of connections the room has already closed, and
		// the symptom is every request answering EOF while the link reports
		// perfectly healthy and four connections idle.
		//
		// A read or write timeout is worse: the terminal websocket and the
		// event stream are meant to be open for hours.
		//
		// What bounds this instead is the link itself. These connections arrive
		// only from a room that authenticated, the hub closes the whole pool
		// when the link drops, and the heartbeat is what notices a dead peer.
		// There is no anonymous client here to defend against.
		ReadHeaderTimeout: 0,
		ReadTimeout:       0,
		WriteTimeout:      0,
		IdleTimeout:       0,
	}
	go func() {
		_ = srv.Serve(&reverseListener{r: r, ctx: ctx})
		// Serve only returns when the listener says it is done, which happens
		// on context cancellation. Nothing to report.
	}()
	defer func() {
		// Closed rather than Shutdown: Shutdown waits for in-flight requests,
		// and an attached terminal is an in-flight request that never ends.
		_ = srv.Close()
	}()

	wait := r.T.Backoff
	for ctx.Err() == nil {
		since := time.Now()
		err := r.attach(ctx)
		if ctx.Err() != nil {
			return nil
		}
		r.setDown(err)
		// BACK TO THE FLOOR AFTER A LINK THAT ACTUALLY WORKED, and this matters
		// more here than in most backoffs.
		//
		// The hub is the half being restarted all evening. Without this, one
		// long outage walks the delay up to the ceiling and it stays there, so
		// every later hub restart costs thirty seconds of blank board even
		// though the hub came back immediately. The person doing the
		// restarting reads that as the link being flaky.
		//
		// "Actually worked" is measured in time attached rather than in the
		// absence of an error, because a link that is refused instantly and one
		// that dies after an hour both return an error and only one of them is
		// a reason to slow down.
		if time.Since(since) > r.T.Silence {
			wait = r.T.Backoff
		}
		if err != nil {
			log.Printf("[link] hub %s: %v (trying again in %s)", r.Dial.Describe(), err, wait)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}
		// Doubling with a ceiling, the same shape the agent loop already uses.
		// A hub that is down for an hour must not be dialled three thousand
		// times, and one that comes back must be found within half a minute.
		if wait *= 2; wait > r.T.BackoffMax {
			wait = r.T.BackoffMax
		}
	}
	return nil
}

// attach runs one control connection until it fails.
func (r *Room) attach(ctx context.Context) error {
	dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
	conn, err := r.Dial.Dial(dialCtx)
	cancel()
	if err != nil {
		return err
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	w, err := sayHello(conn, br, hello{
		Kind: "control", Room: r.Name, Version: r.Version, Host: r.Host,
	})
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.session, r.up, r.since, r.lastErr = w.Session, true, time.Now(), ""
	r.mu.Unlock()
	log.Printf("[link] attached to hub %s as %q", r.Dial.Describe(), r.Name)

	// The warm pool, brought up before anything asks. The first request after
	// a hub restart should not pay for a dial, and the hub restarting is the
	// thing this whole feature exists to make cheap.
	warm := w.Warm
	if warm <= 0 {
		warm = r.T.Warm
	}
	r.open(ctx, warm, w.Session)

	// Two goroutines and this one waits. The beat has to keep writing while
	// the reader blocks, and the reader has to notice a closed socket while the
	// beat sleeps.
	ctx, stop := context.WithCancel(ctx)
	defer stop()
	go r.beat(ctx, conn)

	for {
		var n note
		if err := readJSON(br, &n); err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch {
		case n.Bye != "":
			return errors.New("the hub said: " + n.Bye)
		case n.Need > 0:
			// ASKED FOR, NOT GUESSED AT. The hub knows how many connections it
			// is holding and how many it wants spare. A room dialling on its
			// own schedule would either starve the pool under load or hold
			// sockets open that nothing will ever use.
			r.open(ctx, n.Need, w.Session)
		}
	}
}

// beat says the room is still here, on the control connection.
func (r *Room) beat(ctx context.Context, conn net.Conn) {
	t := time.NewTicker(r.T.Beat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := writeJSON(conn, note{Beat: time.Now().UnixMilli()}); err != nil {
				// Closing is how the reader above finds out. Returning quietly
				// would leave it blocked on a socket nothing will ever write
				// to, which is the half-open case the beat exists to catch.
				_ = conn.Close()
				return
			}
		}
	}
}

// open dials n data connections and hands them to the listener.
//
// Each dial is its own goroutine because a hub asking for four should get four
// in one round trip rather than four in series, and over an overlay a dial is
// not free.
func (r *Room) open(ctx context.Context, n int, session string) {
	for i := 0; i < n; i++ {
		go func() {
			dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
			conn, err := r.Dial.Dial(dialCtx)
			cancel()
			if err != nil {
				// One failed data dial is not a reason to tear the link down.
				// The hub will ask again when it still has no connection, and
				// the control connection is the thing that decides whether the
				// link is alive.
				return
			}
			br := bufio.NewReader(conn)
			if _, err := sayHello(conn, br, hello{
				Kind: "data", Room: r.Name, Session: session,
			}); err != nil {
				conn.Close()
				return
			}
			// THE BUFFERED READER GOES WITH IT. `sayHello` may have pulled the
			// first bytes of the hub's HTTP request into the buffer along with
			// the welcome line, and handing over the bare conn would lose them.
			select {
			case r.conns <- &buffered{Conn: conn, br: br}:
			case <-ctx.Done():
				conn.Close()
			}
		}()
	}
}

func (r *Room) setDown(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.up, r.session = false, ""
	if err != nil {
		r.lastErr = err.Error()
	}
}

// State is what the room's own board can show about its hub.
type State struct {
	Up    bool      `json:"up"`
	Hub   string    `json:"hub"`
	Since time.Time `json:"since,omitempty"`
	Error string    `json:"error,omitempty"`
}

// State reports the link, for the room's own board to draw. A room whose hub is
// down should say so on its own address rather than looking healthy.
func (r *Room) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := State{Up: r.up, Hub: r.Dial.Describe(), Error: r.lastErr}
	if r.up {
		s.Since = r.since
	}
	return s
}

// ── the listener that dials ─────────────────────────────

// reverseListener is the whole trick, and it is nine lines.
//
// `http.Server` asks a listener for connections. This one hands it connections
// the room DIALLED. Everything above exists to keep that channel fed.
type reverseListener struct {
	r   *Room
	ctx context.Context
}

func (l *reverseListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.r.conns:
		return c, nil
	case <-l.ctx.Done():
		return nil, net.ErrClosed
	}
}

func (l *reverseListener) Close() error { return nil }

// Addr is asked by http.Server for logging and by nothing else here. It has to
// answer something, and what it answers should say what this is rather than
// pretending to be a port.
func (l *reverseListener) Addr() net.Addr { return hubAddr(l.r.Dial.Describe()) }

type hubAddr string

func (a hubAddr) Network() string { return "atrium-link" }
func (a hubAddr) String() string  { return string(a) }

// buffered carries a bufio.Reader that has already read ahead of the socket.
type buffered struct {
	net.Conn
	br *bufio.Reader
}

func (b *buffered) Read(p []byte) (int, error) { return b.br.Read(p) }
