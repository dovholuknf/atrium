package link

import (
	"bufio"
	"context"
	"errors"
	"log"
	"net"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
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
	// Commit is the commit this room was built from, sent in the hello. Empty says nothing.
	Commit string
	// T is the timing set. Zero fields take defaults.
	T Timings
	// Upgrades is whether this room will consider a build its hub offers, and
	// what to do with one it has fetched and checked. Nil means no, which is
	// the default: a hub may say what it is running and nothing happens. See
	// `upgrade.go`.
	Upgrades *Upgrades
	// OnRestart is what the room does when its hub asks it to restart itself.
	// Nil ignores the ask, which is the default and is right for a room with no
	// restarter wired: a hub cannot make a room restart, it can only ask. See
	// RestartAsk and cmd/atrium2's wiring.
	OnRestart func(RestartAsk)
	// OnAttach is told each time this room attaches, in its own goroutine. It is
	// how held cross-room messages go the moment the hub is back rather than at
	// the next tick. Nil does nothing. See relay.go.
	OnAttach func()
	// Git says this room takes git syncs, so it tells its hub in the hello. Set only when
	// the room's handler serves /v1/git/sync. See git.go.
	Git bool
	// IdleCPU reads this machine's idle CPU percent for the beat to carry to the hub. Nil, or ok false, says
	// nothing and the hub counts the room as unknown.
	IdleCPU func() (float64, bool)
	// Login says why this room's claude login needs doing, "" when it does not. Nil says nothing.
	Login func() string
	// OnPaths is handed the hub's list of transports, in its order, each time a
	// welcome carries one, in its own goroutine. It is how the room writes the
	// fallback down for a start when the hub cannot be reached. Nil ignores it.
	OnPaths func([]Path)
	// OnUnreachable runs after a climb on which every rung failed, so the room reads its hub's address from disk again.
	OnUnreachable func()
	// RemoteDir is the room's key directory, where remote_access.json and its log live. Empty means remote access is
	// off and nothing is recorded. See stream.go.
	RemoteDir string

	// smu guards streams, the ssh streams this room is carrying, and accessPoke wakes the switch watcher when one
	// opens or closes. wmu serialises writes on the control connection, which the beat and the watcher share.
	smu     sync.Mutex
	streams map[string]*openStream
	// serving counts the serveSSH handlers still running, which write to RemoteDir after their stream ends.
	serving    sync.WaitGroup
	accessPoke chan struct{}
	wmu        sync.Mutex

	// conns carries dialled connections to the listener's Accept. Buffered by
	// one so a dial that wins a race is not thrown away.
	conns chan net.Conn
	// session is what the hub called this connection's lifetime. Cleared on
	// every reconnect, since a new control connection is a new session and a
	// data connection carrying a stale one must be refused.
	mu      sync.Mutex
	session string
	up      bool
	// hubGit is whether the hub said, in its welcome, that it serves git.
	hubGit bool
	// claimRT is the transport PR claims go over, made once. See prclaim.go.
	claimRT http.RoundTripper
	since   time.Time
	lastErr string
	// tried is every address the last climb dialled when none answered, cleared by a climb that gets anywhere.
	tried []string
	// dataErr is the last data dial that failed, cleared by one that works, and
	// dataSaid is when one was last logged. A control connection that is up
	// while every data dial fails is a room that looks attached and serves
	// nothing, and before this it said nothing at all.
	dataErr  string
	dataSaid time.Time
	// dials are the data dials in flight, dataFails the failures in a row, and dropLink closes the current control
	// connection. See dials.go.
	dials map[*dialSlot]struct{}
	// owed is how many asked-for data dials found every slot taken, paid as slots free. See dials.go.
	owed      int
	dataFails int
	dropLink  func()
	// taking makes sure one offer is acted on once, however many times the hub
	// repeats it across reconnects.
	taking taker
	// restarting makes sure one restart is acted on at a time, so two
	// restart_atrium asks in quick succession do not both spawn a detached
	// restarter racing to bind the same address. See restarter and the Restart
	// case in attach.
	restarting restarter
	// paths is the last probe of every rung, and pmu guards it. See pathstate.go.
	pmu   sync.Mutex
	paths []PathState
	// PersistOff writes the kinds that are off down, so a restart keeps the choice. Nil keeps it in memory only.
	PersistOff func(off []string) error
	// tmu serialises switching a transport. forceMove makes the next attach end read as moving, not failing, so the
	// room climbs onto another transport at once when the one it stood on was turned off.
	tmu       sync.Mutex
	forceMove atomic.Bool
}

// restarter is the idempotent guard on OnRestart, the same shape as `taker` and
// for the same reason: the ask can arrive more than once, and acting on every
// copy spawns racing restarters.
//
// It is a one-at-a-time flag rather than a once, because a restart can be asked
// for, declined (agents still busy and no force), and then asked for again, and
// the second ask must not be swallowed by the first having been considered.
// `finish` clears it when the handler returns, which on the path that actually
// restarts is moot because the process is already going down.
type restarter struct {
	mu   sync.Mutex
	busy bool
}

func (r *restarter) start() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.busy {
		return false
	}
	r.busy = true
	return true
}

func (r *restarter) finish() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.busy = false
}

// Run keeps a room attached to its hub until the context is cancelled.
//
// IT NEVER RETURNS AN ERROR FOR A HUB BEING DOWN, and that is the posture the
// whole daemon already takes for things it does not control. A hub that is not
// answering is a room that keeps working with nobody watching it, which is
// exactly what it was before anybody built a hub. So this logs, backs off and
// tries again, and the room's own board never notices.
func (r *Room) Run(ctx context.Context) error {
	r.T = r.T.Fill()
	r.conns = make(chan net.Conn, 1)
	r.accessPoke = make(chan struct{}, 1)

	// The board server, on connections we dialled. Started once and left
	// running: it takes connections off the channel for the life of the room,
	// across any number of hub restarts, because an http.Server does not care
	// where its listener gets sockets from.
	srv := newLinkServer(r.linkHandler())
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
		// MOVING UP IS NOT A FAILURE. The room let go on purpose because a better
		// transport answered, so it climbs again straight away.
		if errors.Is(err, errMovingUp) {
			r.setDown(nil)
			log.Printf("[link] a better transport than %s answers, moving to it", r.Dial.Describe())
			wait = r.T.Backoff
			continue
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
		var u *unreachable
		if errors.As(err, &u) && r.OnUnreachable != nil {
			r.OnUnreachable()
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
	var conn net.Conn
	var err error
	cl, climbs := r.Dial.(climber)
	if climbs {
		// MORE THAN ONE TRANSPORT. Walked from the best down, each bounded by
		// DialWait, and every later connection follows the one that answered.
		conn, err = cl.Climb(ctx, r.T.DialWait)
	} else {
		dialCtx, cancel := context.WithTimeout(ctx, r.T.DialWait)
		conn, err = r.Dial.Dial(dialCtx)
		cancel()
	}
	if err != nil {
		return err
	}
	defer conn.Close()

	br := bufio.NewReader(conn)
	w, err := sayHello(conn, br, hello{
		Fallback: climbs && cl.Fallback(),
		Kind:     "control", Room: r.Name, Version: r.Version, Host: r.Host, Commit: r.Commit,
		// What this room would run, and whether it is willing to be told about
		// a newer one. Neither gives the hub any power: a hub cannot install
		// anything, and this only saves it from offering a Linux room a
		// Windows binary. See `upgrade.go`.
		OS: runtime.GOOS, Arch: runtime.GOARCH,
		Upgrades: r.Upgrades != nil && r.Upgrades.Accept,
		Git:      r.Git,
	})
	if err != nil {
		return err
	}

	r.mu.Lock()
	r.session, r.up, r.since, r.lastErr, r.tried = w.Session, true, time.Now(), "", nil
	r.hubGit = w.Git
	r.dropLink = func() { _ = conn.Close() }
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
	if pa, ok := r.Dial.(pather); ok {
		go r.watchPaths(ctx, pa)
	}
	// WHAT THE REMOTE ACCESS SWITCH SAYS, at attach and on every change. See stream.go.
	go r.watchAccess(ctx, func(n note) error { return r.say(conn, n) })
	// WHAT IS HERE, SAID ONCE ON ATTACHING AND THEN ON CHANGE. The hub caches
	// it so a board can still show this room when it is not answering. It ends
	// with the attachment, because the next one starts by saying everything
	// again. See `announce.go`.
	//
	// Only to a hub that said it keeps one. A hub with no store is a normal
	// thing and announcing at it would be a failure logged on every change.
	if w.Caches {
		go r.announce(ctx, w.Session)
	}
	if r.OnAttach != nil {
		go r.OnAttach()
	}
	if r.OnPaths != nil && len(w.Paths) > 0 {
		go r.OnPaths(w.Paths)
	}
	// ON A FALLBACK, KEEP ASKING THE BETTER ONES. Closing the control
	// connection is how the reader below learns it is time to move.
	var moving atomic.Bool
	//
	// Always started on a climbing dialer, not only when attached on a fallback: the
	// hub's order can change while this room stands on the top rung, and the rung
	// it stands on is then no longer the first. The probe looks at that each tick.
	if climbs {
		go r.probe(ctx, cl, func() {
			moving.Store(true)
			_ = conn.Close()
		})
	}

	keepAlive(conn)
	for {
		var n note
		if err := readNote(conn, br, r.T.Silence, &n); err != nil {
			if moving.Load() || r.forceMove.Swap(false) {
				return errMovingUp
			}
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		switch {
		case n.Bye != "":
			return errors.New("the hub said: " + n.Bye)
		case len(n.Paths) > 0:
			// The hub's addresses changed while this room was attached. Heard as the welcome's list is.
			if r.OnPaths != nil {
				go r.OnPaths(n.Paths)
			}
		case n.Offer != nil:
			// A hub saying what it is running. Considered, which usually means
			// ignored. See `consider` in upgrade.go.
			r.consider(ctx, n.Offer)
		case n.Need > 0:
			// ASKED FOR, NOT GUESSED AT. The hub knows how many connections it
			// is holding and how many it wants spare. A room dialling on its
			// own schedule would either starve the pool under load or hold
			// sockets open that nothing will ever use.
			r.open(ctx, n.Need, w.Session)
		case n.Restart != nil:
			// A hub asking this room to restart itself. Handled off this loop, in
			// its own goroutine, because the handler parks agents and waits, and
			// blocking the reader would stop the heartbeat and make the room look
			// dead while it does exactly what it was told. Ignored entirely when
			// no restarter is wired: a hub cannot make a room restart.
			if r.OnRestart == nil {
				log.Printf("[link] the hub asked this room to restart, but no restarter is wired")
				break
			}
			// One at a time. The hub can repeat the ask across reconnects, and two
			// in quick succession would otherwise both spawn a detached restarter
			// racing to bind the same address. The loser spins its stop grace and
			// then fails to bind, an orphaned process and confusing logs. See
			// restarter.
			if !r.restarting.start() {
				log.Printf("[link] a restart is already in progress, ignoring the duplicate ask")
				break
			}
			go func(ask RestartAsk) {
				defer r.restarting.finish()
				r.OnRestart(ask)
			}(*n.Restart)
		}
	}
}

// errHubSilent is the room giving up on a control connection that stopped carrying frames.
var errHubSilent = errors.New("the hub has been silent too long")

// readNote reads one frame, giving up after `silence` with nothing arriving.
//
// THE HUB ECHOES EVERY BEAT, so a live control connection delivers a frame every Beat and that is a free
// liveness signal. Without a deadline the reader blocks for as long as the kernel keeps retransmitting into a
// black-holed path, which is minutes, and nothing tells the ladder to run. The deadline is set again before every
// frame, so any frame, echo or not, restarts it. A zero silence sets none. See
// docs/fabric/multi-transport-design.md, "The five minutes".
func readNote(conn net.Conn, br *bufio.Reader, silence time.Duration, n *note) error {
	if silence > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(silence))
	}
	err := readJSON(br, n)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return errHubSilent
	}
	return err
}

// keepAlive turns on TCP keepalive when the control connection is a TCP one, or a TLS one over TCP. A second
// layer under readNote that costs nothing, and does nothing for an overlay circuit, which is not a kernel socket.
func keepAlive(conn net.Conn) {
	for i := 0; i < 4; i++ {
		switch c := conn.(type) {
		case *net.TCPConn:
			_ = c.SetKeepAliveConfig(net.KeepAliveConfig{
				Enable: true, Idle: 5 * time.Second, Interval: 5 * time.Second, Count: 3,
			})
			return
		case interface{ NetConn() net.Conn }:
			conn = c.NetConn()
		default:
			return
		}
	}
}

// climber is a Dialer over more than one transport. `Ladder` is the one there is.
type climber interface {
	Climb(ctx context.Context, wait time.Duration) (net.Conn, error)
	Probe(ctx context.Context, wait time.Duration) bool
	Fallback() bool
	Transport() string
}

// errMovingUp is the room letting go of a fallback on purpose.
var errMovingUp = errors.New("moving to a better transport")

// probeNeed is how many good probes in a row move a room back up.
const probeNeed = 2

// probe tries the better transports every ProbeEvery and calls `move` once
// enough of them in a row answered. A failed probe starts the count again.
func (r *Room) probe(ctx context.Context, cl climber, move func()) {
	t := time.NewTicker(r.T.ProbeEvery)
	defer t.Stop()
	good := 0
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
		if !cl.Fallback() {
			good = 0
			continue
		}
		if cl.Probe(ctx, r.T.DialWait) {
			good++
		} else {
			good = 0
		}
		if good >= probeNeed {
			move()
			return
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
			n := note{Beat: time.Now().UnixMilli()}
			if r.IdleCPU != nil {
				if v, ok := r.IdleCPU(); ok {
					n.IdleCPU = &v
				}
			}
			if r.Login != nil {
				v := r.Login()
				n.Login = &v
			}
			n.PathStates = r.pathsNow()
			if err := r.say(conn, n); err != nil {
				// Closing is how the reader above finds out. Returning quietly
				// would leave it blocked on a socket nothing will ever write
				// to, which is the half-open case the beat exists to catch.
				_ = conn.Close()
				return
			}
		}
	}
}

// say writes one frame on the control connection, never through the middle of another.
func (r *Room) say(conn net.Conn, n note) error {
	r.wmu.Lock()
	defer r.wmu.Unlock()
	return writeJSON(conn, n)
}

// open dials up to n data connections and hands them to the listener.
//
// Each dial is its own goroutine because a hub asking for four should get four in one round trip. They are bounded:
// at most maxDataDials are in flight, and an ask that finds none free is owed, not dropped: the hub asks once per waiter
// and a dropped ask left a request waiting its whole DialWait. See dials.go.
func (r *Room) open(ctx context.Context, n int, session string) {
	for i := 0; i < n; i++ {
		s, ok := r.startDial()
		if !ok {
			r.owe(n - i)
			return
		}
		go r.dataDial(ctx, session, s)
	}
}

// dataSayEvery bounds the data dial failure line. The hub asks again on every
// request that finds the pool short, and a board open on a starved room would
// otherwise write one line per request.
const dataSayEvery = time.Minute

// dataFailed records a data dial that failed and logs it, at most once a
// dataSayEvery.
func (r *Room) dataFailed(err error) {
	r.mu.Lock()
	r.dataErr = err.Error()
	r.dataFails++
	redial := r.dataFails >= dataRedialAfter
	say := time.Since(r.dataSaid) >= dataSayEvery
	if say {
		r.dataSaid = time.Now()
	}
	r.mu.Unlock()
	if redial {
		r.redialLink()
	}
	if say {
		log.Printf("[link] a data connection to hub %s failed: %v. the control connection is up, "+
			"so the room looks attached and serves nothing until one gets through", r.Dial.Describe(), err)
	}
}

// dataWorked clears the failure once a data dial gets through.
func (r *Room) dataWorked() {
	r.mu.Lock()
	was := r.dataErr
	r.dataErr, r.dataSaid, r.dataFails = "", time.Time{}, 0
	r.mu.Unlock()
	if was != "" {
		log.Printf("[link] data connections to hub %s get through again", r.Dial.Describe())
	}
}

func (r *Room) setDown(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.up, r.session, r.hubGit = false, "", false
	if err != nil {
		r.lastErr = err.Error()
		r.tried = nil
		var u *unreachable
		if errors.As(err, &u) {
			r.tried = u.tried
		}
	}
}

// State is what the room's own board can show about its hub.
type State struct {
	Up    bool      `json:"up"`
	Hub   string    `json:"hub"`
	Since time.Time `json:"since,omitempty"`
	Error string    `json:"error,omitempty"`
	// Transport is the kind the room is on, or last tried. Empty for a room with
	// one transport, which has nothing to choose between.
	Transport string `json:"transport,omitempty"`
	// Fallback says it is on a transport below its first choice.
	Fallback bool `json:"fallback,omitempty"`
	// DataError is the last data dial that failed while the control connection
	// is up. Empty once one gets through.
	DataError string `json:"data_error,omitempty"`
	// Room is this room's name, for the board's sentence about it.
	Room string `json:"room,omitempty"`
	// Unreachable is every address the last climb tried when none answered. Empty while any does.
	Unreachable []string `json:"unreachable,omitempty"`
}

// State reports the link, for the room's own board to draw. A room whose hub is
// down should say so on its own address rather than looking healthy.
func (r *Room) State() State {
	r.mu.Lock()
	defer r.mu.Unlock()
	s := State{Up: r.up, Hub: r.Dial.Describe(), Error: r.lastErr, Room: r.Name}
	if !r.up {
		s.Unreachable = r.tried
	}
	if cl, ok := r.Dial.(climber); ok {
		s.Transport, s.Fallback = cl.Transport(), cl.Fallback()
	}
	if r.up {
		s.Since, s.DataError = r.since, r.dataErr
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
	// via is the transport whose listener took a pooled data connection, set by the hub. Empty elsewhere.
	via string
}

func (b *buffered) Read(p []byte) (int, error) { return b.br.Read(p) }

// CloseWrite half closes the connection under the buffer where it can, so a stream end passes through intact.
func (b *buffered) CloseWrite() error {
	closeWrite(b.Conn)
	return nil
}

func newLinkServer(h http.Handler) *http.Server {
	return &http.Server{
		// THE BOARD'S HANDLER WITH `/_link/ssh` IN FRONT, on the link only. See linkHandler.
		Handler: h,
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
}
