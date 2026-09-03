# The forum: implementation plan

`docs/federation-design-v2.md` is the accepted design and nothing here re-opens it. The forum holds nothing, leaves
dial out to it, every leaf keeps owning every card, the command is `atrium forum` and a leaf is an atrium. This
document turns that verdict into stages someone can build, in order, each one shippable on its own.

Every claim about existing code below was read at the current working tree. The tree has uncommitted changes in
`internal/daemon`, `internal/api` and `internal/store`, so line numbers may drift by a few lines. Nothing here was
run. Where a claim could not be verified by reading, it says so.

Section order differs from the design's. The transport and the routing table come before the stages, because stages
1 and 2 are the transport and the routing table, and reading the stage list first means reading it twice.

```
  leaf: atrium daemon                              forum: atrium forum
  ┌────────────────────────────┐                   ┌──────────────────────────────┐
  │ :7777 agents   (loopback)  │                   │ :7779 leaf acceptor          │
  │ :7778 board    (loopback)  │                   │ :7780 board, one origin      │
  │ sqlite, gate, ptys, reaper │ ─ dials out ────▶ │ table: name -> peer, in mem  │
  │                            │                   │ one ReverseProxy per peer    │
  │ http.Serve(dialled,        │ ◀─ requests ───── │ no sqlite, no halt, no card  │
  │            d.ap.Handler()) │    back down      └──────────────────────────────┘
  └────────────────────────────┘                                 ▲
      unchanged and fully usable                                 │
      at localhost:7778 alone                            browser, one address
```

## Stage 0: what already exists, and is genuinely free

Five things. Each one is the reason a stage that would otherwise be a week is a day.

**The leaf's whole board is one `http.Handler`.** `internal/daemon/daemon.go:487` builds the human server as
`humanSrv := &http.Server{Addr: d.opts.HumanAddr, Handler: d.ap.Handler()}`, and `api.Server.Handler()`
(`internal/api/api.go:118-172`) is a single `http.ServeMux` carrying all 40-odd routes plus the embedded board at
`mux.Handle("/", webHandler())`. Serving that same handler on a connection the leaf dialled out is
`http.Serve(dialledListener, d.ap.Handler())`. There is no second representation of a card anywhere in the design
because there is no second handler.

**The board funnels every request through one function.** `internal/api/web/index.html:2104-2112` defines
`const api = async (path, opts) => { const res = await fetch(path, opts); ... }` and it is the only `fetch(` call
site in a 6906 line file. All 53 API calls go through it. A per-peer base URL is one line inside `api()`, plus the
`EventSource("/v1/events")` at line 5610 and the WebSocket at 5849-5850. This is the single largest piece of free
work in the whole plan, and it was not free by accident.

**The overlay child-process pattern is the shape a link should copy, and the shape it should not.**
`internal/daemon/overlay.go` holds one long-lived thing per kind, tracks `starting` separately from `cmd` because
starting is slow and cannot be done under the mutex (`overlay.go:72-77`), separates `stopping` from failing so a
deliberate stop does not paint
a red box (`overlay.go:78-81`), keeps a bounded tail of output (`keepLines = 200`), and reports itself through a
plain struct the board renders (`OverlayState`, `overlay.go:46-64`). The forum link should reuse that state and
reporting shape verbatim. It should not reuse `exec.Cmd`: a link is a goroutine and a socket, not a child process.

**SSE is a nudge, not a delta, and the board polls behind it.** `internal/api/api.go:877-917` emits
`event: <kind>\ndata: <json>\n\n` and a bare `: ping\n\n` every 25 seconds. The page wires `task`, `task-removed`,
`permission` and `halted` straight to `refresh()` and ignores the payload (`index.html:5615`), with
`setInterval(refresh, 5000)` behind it (`index.html:6903`). A missed event costs one stale render. Nothing needs
`Last-Event-ID`, per-peer sequencing, or replay across a reconnect. Three events do carry deltas and are applied
directly (`overlays`, `settings`, `hooks`, `index.html:5621-5633`), which matters only in that those three are
per-peer facts and must not be merged across peers.

**Ages are already seconds computed by the machine that owns the clock.** `/v1/tasks` returns `idle_seconds` and
`wait_seconds` derived from `time.Since` on the daemon (`internal/api/api.go:242`, `:252`). Merging a stack across
machines needs no clock reconciliation, because no instant crosses the wire in a form anyone re-derives from.

**A settings table already exists and takes arbitrary keys.** `internal/store/settings.go:19-45` gives
`Setting(key)` and `SetSetting(key, value)` over the `setting` table created by migration `0018_setting`
(`internal/store/schema.go:368`). Forum configuration needs no migration at all. See "Migration and compatibility".

What is **not** free, and is the honest counterweight: there is no reverse-connection machinery of any kind in the
tree, no `httputil` import anywhere, and the board has no concept of an origin other than its own. Those three are
the actual work.

## The transport

The requirement is exact. The leaf opens the connection. The forum then sends ordinary HTTP requests down it,
including an SSE stream that must not be buffered and a WebSocket upgrade that must survive. The leaf answers with
`d.ap.Handler()` and nothing else.

### Candidate 1: pooled dialled TCP connections, plus `httputil.ReverseProxy`

The leaf dials several plain TCP connections to the forum and pushes each into a channel-backed `net.Listener`. One
`http.Server` serves them all. The forum reads a one-line handshake off each arriving connection, files it under a
peer, and builds one `http.Transport` per peer whose `DialContext` pops a connection from that peer's pool instead
of dialling anything. `httputil.ReverseProxy` sits on top of that transport.

New dependencies: none. `net`, `net/http` and `net/http/httputil` are stdlib. `ReverseProxy` already does the two
hard parts: `FlushInterval: -1` flushes after every write, which is what SSE needs, and it hijacks and copies both
directions when the upstream answers `101 Switching Protocols`, which is what attach needs. Neither has to be
written.

The cost is connection accounting. Every open SSE stream and every open attach socket pins one connection for its
lifetime, so the leaf has to keep spare connections dialled without being asked for them.

### Candidate 2: yamux over one dialled connection

`github.com/hashicorp/yamux` multiplexes streams over a single connection. The leaf dials once, wraps the
connection in `yamux.Server`, and serves `http.Serve(session, d.ap.Handler())` because a yamux session already
satisfies `net.Listener`. The forum's `DialContext` becomes `session.Open()`. One connection per leaf, no pool, no
accounting, and yamux ships keepalives.

Checked against `go.mod` at the current tree: yamux is not present, direct or indirect. It would be a new direct
dependency for a repo whose direct list is eight modules long. The design's own note (`federation-design-v2.md`
section 9, stage 2) says the same thing and reaches the same conclusion.

### Candidate 3: one WebSocket per HTTP connection, over `coder/websocket`

`websocket.NetConn` turns a WebSocket into a `net.Conn`, so the leaf could dial `wss://forum/link` N times and feed
those into the same channel-backed listener. `github.com/coder/websocket v1.8.15` is already in `go.mod`, though
listed under the indirect block despite `internal/daemon/attach.go:11` importing it directly, which looks like a
stale `go mod tidy` and is worth fixing regardless. The real benefit is that the link becomes ordinary HTTPS and
survives a corporate proxy or any HTTP reverse proxy the operator puts in front of the forum. The cost is per-frame
masking on every byte, and an attach socket then rides a WebSocket inside a WebSocket.

### The pick

**Candidate 1.** Three reasons, in order of weight.

It adds no dependency, and the design's whole argument is that this component is small. Adding a multiplexer to a
process whose job is to hold nothing is the wrong first move.

The leaf side is literally the one line the design promises. `http.Serve(l, d.ap.Handler())`. Candidate 2 is also
one line, but only after a dependency and a session wrapper. Candidate 1 makes the hinge visible in the code.

`ReverseProxy` already solves streaming and upgrade, and it is the part most likely to be got wrong by hand. A
hand-rolled proxy that buffers SSE is a bug that shows up as "the board is stale sometimes" and takes a day to find.

The swap trigger is written down now so nobody has to argue about it later. **Move to candidate 2 when either the
observed connection count per leaf exceeds roughly thirty in normal use, or the forum has to sit behind an HTTP
proxy that will not carry raw TCP.** The swap changes `Acceptor` and the leaf's dialler and nothing above them: the
routing table, the proxy, the board and every stage from 3 onward are unaffected, because all of them see a
`net.Listener` and an `http.RoundTripper`.

### The connection pool, concretely

This is the part the design lists as asserted rather than prototyped, so it is written out.

The leaf keeps a target number of **spare** connections dialled. A connection stops being spare the moment the
forum writes a request on it, and the leaf learns that from the first byte it reads. Nothing else signals, which
means there is no second protocol between forum and leaf beyond the handshake.

```go
// internal/forumlink/pool.go

// watchedConn tells the pool when a connection stops being spare.
//
// The forum takes a connection out of the pool by writing a request on it, and
// the leaf finds out the same way: the first byte read. Every refill is driven
// from here, so the forum never has to ask for more connections and there is no
// second protocol.
type watchedConn struct {
	net.Conn
	once sync.Once
	used func()
}

func (c *watchedConn) Read(b []byte) (int, error) {
	n, err := c.Conn.Read(b)
	if n > 0 {
		c.once.Do(c.used)
	}
	return n, err
}
```

```go
// dialledListener turns an outbound dialler into something http.Serve accepts
// from. Every connection the link opens goes here, so one http.Server serves
// them all and how many to hold open is a pool decision rather than a serving
// decision.
type dialledListener struct {
	conns  chan net.Conn
	closed chan struct{}
	addr   net.Addr
}

func (l *dialledListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case <-l.closed:
		return nil, net.ErrClosed
	}
}

func (l *dialledListener) Close() error {
	select {
	case <-l.closed:
	default:
		close(l.closed)
	}
	return nil
}

func (l *dialledListener) Addr() net.Addr { return l.addr }
```

Two numbers, both configurable and both with a stated default. `minSpare` defaults to 4, which covers one SSE
stream, one attach socket and two concurrent short requests without a redial on the critical path. `maxConns`
defaults to 32, which is the cap that turns a runaway into a log line instead of a file descriptor exhaustion.

One behaviour to expect and not be surprised by: the forum's `http.Transport` keeps connections alive and reuses
them, so a connection that carried one request returns to the transport's idle set rather than to the leaf's spare
set. The steady state per leaf is therefore roughly `minSpare` plus the transport's idle count plus one per open
stream. That number is worth exposing, and stage 1 exposes it, because "is the pool sized right" is a question with
a number for an answer.

## The routing table

The forum holds one map and nothing else.

```go
// internal/forum/table.go

// peer is one atrium reporting to this forum. Everything here is derived from
// live connections. Nothing is written down, and nothing survives the process,
// because a forum that remembered a peer would be claiming a fact it cannot
// check.
type peer struct {
	// Name is what this atrium calls itself: the --as value, or the identity
	// the listener carried when the inbound side supplies one.
	Name string
	// Instance is generated once per daemon process at startup. It is what
	// separates "the same atrium reconnecting" from "a different atrium
	// claiming a taken name".
	Instance string
	// Board is the leaf's own board address, so the forum can offer a link
	// that bypasses it. A forum that cannot be bypassed is a single point of
	// failure with a nicer name.
	Board string
	// Version is the leaf's build, reported so a stale leaf is diagnosable.
	Version string

	Since    time.Time
	LastSeen time.Time

	pool  *connPool
	proxy *httputil.ReverseProxy
}

// table is the whole of the forum's state.
type table struct {
	mu sync.RWMutex
	by map[string]*peer // keyed by peer.Name
}
```

Keyed by `Name`, the value the leaf sent in its handshake, lowercased and validated against
`^[a-z0-9][a-z0-9._-]{0,62}$` so it is safe in a URL path segment and cannot contain a slash.

The handshake is one line of JSON with a trailing newline, written by the leaf as the first thing on every
connection it dials, read by the forum before the connection is handed to a pool. After that line the connection
carries HTTP and nothing else.

```go
// internal/forumproto/hello.go

// Hello is the first line on every connection a leaf dials.
type Hello struct {
	Proto    int    `json:"proto"` // 1
	Peer     string `json:"peer"`
	Instance string `json:"instance"`
	Board    string `json:"board,omitempty"`
	Version  string `json:"version,omitempty"`
}

// Welcome is the forum's one-line answer. A refusal is answered rather than
// dropped, so a leaf whose name is taken says so in its own log instead of
// looking like a network fault.
type Welcome struct {
	OK     bool   `json:"ok"`
	Reason string `json:"reason,omitempty"`
}
```

### Two leaves claiming one name

`wire_name` collides across machines and always will (`internal/store/schema.go:35,38`, and the join path takes
`filepath.Base(cwd)` with no suffix at `internal/cli/join.go:64-77`). That is a per-leaf bug and the forum neither
causes nor fixes it. What the forum must answer is the different question of two **atria** claiming one peer name,
which is a routing question and has to have a decision.

Three cases, distinguished by `Instance`.

| Arriving | Slot holds | What the forum does |
| --- | --- | --- |
| name `laptop`, instance `A` | nothing | Create the slot. Log `peer laptop joined`. |
| name `laptop`, instance `A` | instance `A` | Add the conn to the existing pool. Update `LastSeen`. Log nothing. |
| name `laptop`, instance `B` | instance `A`, live | Refuse with `Welcome{OK:false}`. Log per instance, not per conn. |
| name `laptop`, instance `B` | instance `A`, no live conns | Replace the slot. Close what is left of `A`. Log it. |

The last row is what makes a restarted daemon work. A daemon that restarts gets a new instance, and its old slot has
no live connections because the old process is gone, so it takes the name back on its first reconnect rather than
being locked out by its own corpse.

Row three is deliberate and it is the honest limit of answer (i) in the design's section 4. Any leaf that can reach
the forum can claim any free name. Reachability is the authorisation. First claim wins, and the second is told why.
Stage 7 replaces `Name` with something the listener guarantees, at which point row three cannot arise.

A peer is dropped from the table when its pool empties and no connection has arrived for `peerGrace`, default 30
seconds. Dropping on the last connection closing would make a leaf disappear from the board during an ordinary
redial.

## Stage 1: an atrium reports, a forum lists

**What it does and why here.** Builds the two halves of the link and proves the direction, the handshake, the
routing table and the reconnect loop, with nothing forwarded. It is first because every later stage assumes the
link exists and none of them can be debugged while the link is also new. It is one day because the leaf side is a
dial loop and the forum side is a map plus one JSON endpoint.

**Files.**

- `internal/forumproto/hello.go` (new): `Hello`, `Welcome`, the name validator, and the read and write helpers for
  the one-line handshake. A tiny package shared by both sides so neither owns the wire format.
- `internal/forumlink/link.go` (new): the leaf's outbound link. Dial loop, backoff, state reporting.
- `internal/forumlink/pool.go` (new): `dialledListener`, `watchedConn`, the spare-connection accounting.
- `internal/forum/forum.go` (new): the forum process. Acceptor loop, handshake read, board listener.
- `internal/forum/table.go` (new): `peer`, `table`, and the collision rules above.
- `internal/cli/forum.go` (new): `atrium forum`.
- `internal/cli/cli.go` (changed): register `newForum()` in the root at line 44, and add `--forum` and `--as` to
  `newDaemon()` at lines 50-77.
- `internal/daemon/daemon.go` (changed): two `Options` fields, and starting the link goroutine in `Run` next to
  `go d.reap(ctx, ReapEvery)` at line 527.
- `internal/daemon/forum.go` (new): the daemon's side of holding a link, shaped like `overlay.go`.

**Interfaces.**

```go
// internal/daemon/daemon.go, added to Options.

	// Forum is the address of a forum to report to, empty for none. A leaf
	// with no forum is exactly the leaf that shipped before forums existed:
	// the link goroutine never starts and nothing on any hot path branches.
	Forum string
	// ForumName is what this atrium calls itself at that forum. Defaults to
	// os.Hostname(). It is a display label with no authority: see stage 7.
	ForumName string
```

```go
// internal/forumlink/link.go

// Link is one atrium's outbound connection to one forum.
//
// Its failure is logged and never returned. A daemon that refused to start
// because a forum was unreachable would have inverted the entire dependency,
// and the leaf's board, gate, store and ptys do not know this exists.
type Link struct {
	// Addr is the forum's leaf acceptor, host:port.
	Addr string
	// Hello is what this atrium says about itself on every connection.
	Hello forumproto.Hello
	// Handler is what gets served on the connections this link dials. In the
	// daemon it is d.ap.Handler(), which is the whole board API and nothing
	// new.
	Handler http.Handler
	// MinSpare is how many connections to keep dialled and unused. MaxConns
	// caps the total.
	MinSpare, MaxConns int
}

// Run dials, keeps dialling, and serves Handler on whatever it gets. It
// returns only when ctx is cancelled.
func (l *Link) Run(ctx context.Context)

// State is what the board shows for a link. Deliberately the same shape as
// daemon.OverlayState, so the panel that draws a share draws this too.
type State struct {
	// Configured is whether an address was given at all. Everything else is
	// moot when this is false.
	Configured bool      `json:"configured"`
	Addr       string    `json:"addr,omitempty"`
	Connected  bool      `json:"connected"`
	Since      time.Time `json:"since,omitempty"`
	// Conns is how many connections are open, and Spare how many of those
	// have never carried a request. "Is the pool sized right" is a question
	// with a number for an answer, so the number is reported.
	Conns, Spare int `json:"conns"`
	// Attempts counts redials since the last success, so a flapping link is
	// distinguishable from a link that came up once and stayed.
	Attempts int    `json:"attempts"`
	Err      string `json:"err,omitempty"`
}

func (l *Link) State() State
```

```go
// internal/forum/forum.go

// Inbound is one way leaves arrive.
//
// Written this narrow on purpose. net.Listen satisfies it, and so does agora's
// tunnel.Listen, which is the entire cost of keeping stage 7 to one
// constructor. Nothing in the forum may type-assert a connection to
// *net.TCPConn or read a TCP address off it.
type Inbound struct {
	Listener net.Listener
	// Name, when set, is the identity the forum trusts for every connection
	// this listener accepts, and the Peer field of the handshake is ignored.
	// Empty means the handshake is all there is. See stage 7.
	Name string
}

// Options configures a forum. There is no DBPath and there never will be.
type Options struct {
	// LeafAddr is the plain TCP acceptor, used when no Inbound is supplied.
	LeafAddr string
	// BoardAddr is the one address a browser points at.
	BoardAddr string
	// Inbounds replaces LeafAddr when the acceptor comes from somewhere else.
	Inbounds []Inbound
	// PeerGrace is how long a peer with no connections stays listed.
	PeerGrace time.Duration
}

type Forum struct { /* opts, table, board server */ }

func New(opts Options) *Forum
func (f *Forum) Run(ctx context.Context) error
```

The `atrium forum` command, for the record, so nobody invents different flags:

```
atrium forum --listen :7779 --board :7780
```

No `--db`. No `WORKTREE_ROOT`. If either ever appears on this command, rule 1 of the design has been broken.

The forum answers exactly one route in this stage:

```go
// GET /peers
type peerView struct {
	Name      string `json:"name"`
	Instance  string `json:"instance"`
	Board     string `json:"board,omitempty"`
	Version   string `json:"version,omitempty"`
	Since     string `json:"connected_since"`   // RFC3339
	LastSeen  string `json:"last_seen"`         // RFC3339
	Conns     int    `json:"conns"`
	Reachable bool   `json:"reachable"`
}
```

**How you know it works.**

- Two daemons on one machine, different ports and different databases, plus one forum. `curl localhost:7780/peers`
  lists both, each with a distinct `instance` and a non-zero `conns`.
- `Ctrl-C` one daemon. It leaves `/peers` within `peerGrace`, and the other is untouched.
- Kill the **forum**. Both daemons keep serving their own boards and their own agent listeners. Confirm by making a
  real tool call through a gated session and by loading `localhost:7778` while the forum is dead. Then restart the
  forum and watch both reconnect with no daemon restart and no operator action. This check is the design's section 5
  and it is the whole reason this stage exists on its own.
- Start a third daemon with the same `--as` as one already connected. It is refused with the taken-name reason, says
  so once in its own log rather than once per redial, and the connected one is undisturbed.
- Restart the daemon that owns a name. It reclaims the name on its first reconnect.
- Unit test, no network: a `Link` and a `Forum` over `net.Pipe` for the handshake cases, and the collision table
  above as a table-driven test against `table.join()`.

**Not in this stage.** Nothing is forwarded. There is no board on the forum beyond `/peers` as JSON. No
`sharing()` change, because the forum still cannot reach the leaf.

## Stage 2: the forum forwards, and the leaf serves the handler it already has

**What it does and why here.** Makes `/atrium/{peer}/v1/...` at the forum equal `/v1/...` at the leaf, byte for
byte, including SSE. This is stage 2 and not stage 1 because the forwarding path is only debuggable once the link is
known good, and it is not stage 3 because the board is a much larger surface and should be pointed at a proxy that
has already been proven with `curl`.

**This is the stage that closes the shutdown-token hazard, and it must not slip.** The reason is sharper than the
design states. A dialled outbound connection has `RemoteAddr` equal to the **forum's** address, and `net/http` sets
`r.RemoteAddr` from `conn.RemoteAddr()`. So `isLoopback(r)` (`internal/daemon/shutdown.go:51-58`) returns true for
every forwarded request whenever the forum runs on the same machine as the leaf, which is the ordinary development
setup and the first thing anyone will try. `handleShutdown` (`shutdown.go:68-87`) would then accept a
tokenless stop that arrived through the forum from anywhere. `d.sharing()` (`internal/daemon/overlay_api.go:222-229`)
loops over `OverlayZrok` and `OverlayZiti` only and cannot see a link.

**Files.**

- `internal/forum/proxy.go` (new): the per-peer transport and `ReverseProxy`, the path rewrite, the error handler.
- `internal/forum/forum.go` (changed): route `/atrium/{peer}/` into the proxy.
- `internal/daemon/overlay_api.go` (changed): `sharing()` gains the link.
- `internal/daemon/forum.go` (changed): expose `linked()`.
- `internal/daemon/shutdown_share_test.go` (changed) and a new case covering a link with no share running.

**Interfaces.**

```go
// internal/forum/proxy.go

// newPeerTransport dials nothing. Every connection comes out of the pool the
// leaf filled, so the host in the URL is a label that is never resolved.
func newPeerTransport(p *peer) *http.Transport {
	return &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return p.pool.take(ctx)
		},
		// The board holds an SSE stream open per peer, with an attach socket
		// on top of it. A default per-host connection cap would queue the
		// second one behind the first for as long as the first is open, which
		// is forever.
		MaxIdleConnsPerHost: 8,
		IdleConnTimeout:     90 * time.Second,
		// Never compress. SSE and attach are streams, and a compressor in the
		// middle of one buffers it into uselessness.
		DisableCompression: true,
		// No ResponseHeaderTimeout. Every long-lived response here sends its
		// headers immediately, so a timeout would only ever fire on a leaf
		// that is genuinely wedged, and the browser already handles that.
	}
}

func newPeerProxy(p *peer, onErr func(*peer, http.ResponseWriter, error)) *httputil.ReverseProxy {
	prefix := "/atrium/" + p.Name
	return &httputil.ReverseProxy{
		Rewrite: func(r *httputil.ProxyRequest) {
			r.Out.URL.Scheme = "http"
			// Never resolved: DialContext ignores it. It exists because
			// http.Transport keys its connection pool on it.
			r.Out.URL.Host = "atrium." + p.Name + ".invalid"
			r.Out.URL.Path = strings.TrimPrefix(r.In.URL.Path, prefix)
			if r.Out.URL.Path == "" {
				r.Out.URL.Path = "/"
			}
			// Upgrade and Connection have to survive for attach to work.
			r.Out.Header = r.In.Header.Clone()
		},
		Transport: newPeerTransport(p),
		// Flush after every write. SSE is the reason this component exists in
		// the shape it does, and the default buffers it.
		FlushInterval: -1,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			onErr(p, w, err)
		},
	}
}
```

`httputil.ReverseProxy` handles the `101 Switching Protocols` case by hijacking and copying both directions, so the
attach socket needs no separate code path here. That claim is from the stdlib's documented behaviour and was not
exercised against a running leaf. Stage 5 is where it is proven.

```go
// internal/daemon/overlay_api.go, changed.

// sharing reports whether anything is making this board reachable from
// somewhere that is not this machine.
//
// Read by the shutdown endpoint, which cannot trust a loopback source address
// while something on this machine is terminating connections from anywhere.
// A forum link is exactly that, and worse than a tunneler in one respect: a
// connection dialled out to a forum on this same machine reports RemoteAddr
// 127.0.0.1, so isLoopback says yes to a request that came from another
// continent.
func (d *Daemon) sharing() bool {
	for _, k := range []overlayKind{OverlayZrok, OverlayZiti} {
		if d.ovl.get(k).state("").Running {
			return true
		}
	}
	return d.linked()
}

// linked reports whether a forum link is open right now. Configured but
// unreachable does not count: nothing can reach the board through a link that
// is not up.
func (d *Daemon) linked() bool {
	if d.link == nil {
		return false
	}
	return d.link.State().Connected
}
```

The refusal message in `shutdown.go:78-82` needs a second wording, because "a share is running" is wrong and
misleading when the cause is a link. Two branches, two messages, one saying which forum.

**How you know it works.**

- `curl -s localhost:7780/atrium/laptop/v1/tasks | sha256sum` equals `curl -s localhost:7778/v1/tasks | sha256sum`,
  taken close enough together that `idle_seconds` has not ticked. If they differ, compare with the seconds fields
  stripped and fix the rewrite rather than arguing about the clock.
- `curl -N localhost:7780/atrium/laptop/v1/events` receives `: ping` within 25 seconds, and receives an `event: task`
  line within a second of touching a card on the leaf's own board. The delay is the check: an event that arrives in a
  batch after 25 seconds means `FlushInterval` is wrong.
- `curl -X POST localhost:7780/atrium/laptop/v1/shutdown` with no token is refused with `403`, and refused
  **because of the link**, with `zrok` and `ziti` both stopped. Confirm from the message text, which must name the
  forum rather than a share.
- `POST localhost:7778/v1/shutdown` from the leaf's own terminal, with a link open and no token, is also refused.
  This is the behaviour change and it will surprise somebody, so it belongs in `CHANGELOG.md` in this stage.
- A Go test in `internal/forum` that stands a real `api.Server` behind a `Link` over a loopback listener and asserts
  a forwarded `GET /v1/health` matches a direct one. This is cheap and it is the regression net for every later
  stage.

**Not in this stage.** No board is served by the forum. No peer picker. No merging. Attach is expected to work at
the protocol level and is not tested until stage 5.

## Stage 3: the board, served by the forum, one peer at a time

**What it does and why here.** The forum serves the board asset and the page learns that its API lives at a base URL
rather than at the root. Selecting a peer gives exactly today's board pointed somewhere else. This is stage 3 because
it is where the base-URL indirection lands, and that indirection is what keeps client-side federation available
later at no cost. It is before merging because a bug in the prefix must not be debugged at the same time as a bug in
the merge.

**Files.**

- `internal/api/web/index.html` (changed): `api()` at 2104, the `EventSource` at 5610, the WebSocket at 5849-5850,
  and a peer selector.
- `internal/forum/board.go` (new): serve the same embedded asset, plus `/peers`.
- `internal/api/web.go` (changed): export `webHandler()` as `api.WebHandler()` so the forum serves the identical
  bytes rather than a copy.

**Interfaces.** The whole change on the page is three anchors.

```js
// Where the API for the currently selected atrium lives. Empty string means
// this board's own daemon, which is every existing deployment and the reason
// the default is empty rather than "/atrium/local".
let apiBase = "";

const api = async (path, opts) => {
  const res = await fetch(apiBase + path, opts);
  // ... unchanged
};
```

```js
// The SSE stream and the attach socket bypass api() because neither is a
// fetch. Both take the same prefix, and the socket additionally stops
// assuming location.host is the machine holding the pty.
const es = new EventSource(apiBase + "/v1/events");

const proto = location.protocol === "https:" ? "wss:" : "ws:";
termSock = new WebSocket(`${proto}//${location.host}${apiBase}/v1/tasks/${taskID}/attach`);
```

The three `/vendor/` asset tags at `index.html:7-9` and `navigator.serviceWorker.register("/sw.js")` at
`index.html:4177` stay absolute and unprefixed. They are served by whoever served the page, which is correct for
both the leaf and the forum, and prefixing them would send the browser to fetch xterm.js through a proxy to a leaf.

The forum needs one route for these:

```go
// The forum serves the same embedded board the daemon does, from the same
// bytes. A second copy would drift, and the drift would show up as a feature
// that works locally and not through the forum.
mux.Handle("/", api.WebHandler())
```

**How you know it works.** With one atrium reporting and the board opened at `localhost:7780`, every existing board
function works: the stack renders, the detail pane opens, prompting reaches the agent, shelving answers a pending
request, launching starts a runner on the leaf's machine, `/v1/browse` lists the **leaf's** filesystem and not the
forum's, rules save, and the permission queue resolves. `internal/api/browse.go` and `internal/daemon/launch.go` are
the two that prove routing rather than aggregation, so check them explicitly: browse to a directory that exists only
on the leaf. Attach is stage 5 and is expected to fail here.

**Not in this stage.** One peer at a time. No merged stack, no cross-peer alerting, no attach.

## Stage 4: many peers, merged, read then write

**What it does and why here.** Fans the list fetches across every reporting atrium, tags each card with its atrium,
and merges the stack by `wait_seconds`. Then audits every mutation so it posts to the atrium the card came from.
This is stage 4 because merging is meaningless before forwarding works and dangerous before the base URL is a
per-request value, and because the write audit needs somewhere to be wrong: a call site that assumes the local
daemon is invisible until a second peer exists.

**Files.**

- `internal/api/web/index.html` (changed): `refresh()` at 5561-5607 fans out, `repaintLists()` groups, every write
  path takes a peer.
- `internal/forum/events.go` (new): the merged nudge stream. See below, this is a correction to the design's
  staging.
- `internal/forum/board.go` (changed): route it.

**The merged nudge stream, and why it moves to this stage.** The design puts the forum's SSE handling in stage 6 as
an optional optimisation. It cannot wait that long. Browsers cap concurrent HTTP/1.1 connections per origin at six
in Chrome and Firefox. The forum is one origin by construction, so one `EventSource` per peer exhausts the cap at
six atria and the board stops being able to fetch anything at all. That is a hard failure, not a slowdown, and it
arrives exactly when the feature starts being worth having.

The fix keeps rule 6 intact. The forum opens one upstream `/v1/events` per peer, reads only the blank-line framing
that separates SSE messages, and emits one merged stream in which every message is the same:

```go
// internal/forum/events.go

// A forum event says which atrium had something happen and nothing else.
//
// The board treats an event as a nudge and refetches (index.html:5615), so the
// payload was never load-bearing. Dropping it is what lets the forum carry N
// upstream streams on one connection to the browser without decoding a single
// card, which is rule 6.
//
//	event: nudge
//	data: {"peer":"laptop","kind":"task"}
//
// The forum reads the "event:" line and the blank line that ends a message. It
// never reads "data:".
type nudge struct {
	Peer string `json:"peer"`
	Kind string `json:"kind"`
}
```

Three of the leaf's events do carry deltas the page applies directly: `overlays`, `settings` and `hooks`
(`index.html:5621-5633`). Those are per-atrium facts. The merged stream must carry them as nudges too, and the page
must refetch the corresponding endpoint for the named peer rather than applying a payload that might belong to a
different machine. This is a real change to those three handlers and it is the only place in the whole plan where
the board's event handling logic changes rather than moving.

**Interfaces, on the page.**

```js
// One entry per reporting atrium. `base` is what api() prefixes. `ok` is
// whether the last fan-out reached it. A peer that failed is rendered as
// failed, never dropped: cards vanishing silently is the failure this whole
// project exists to prevent.
let peers = []; // [{name, board, base, ok, err}]

// Fan out, render what arrived, mark what did not. Never wait for the slowest.
async function fanout(path) {
  const results = await Promise.allSettled(
    peers.map(p => api(path, undefined, p)));
  return results.map((r, i) => ({
    peer: peers[i],
    ok: r.status === "fulfilled",
    value: r.status === "fulfilled" ? r.value : null,
    err: r.status === "rejected" ? String(r.reason) : "",
  }));
}
```

`api()` grows an optional third argument rather than reading a global, because a fan-out has several bases in flight
at once and a module-scoped `apiBase` is a race with the peer picker:

```js
const api = async (path, opts, peer) => {
  const base = peer ? peer.base : apiBase;
  const res = await fetch(base + path, opts);
  // ... unchanged
};
```

**The write audit.** Every one of these must carry the peer of the card it acts on, and each is a distinct call site
on the page today: prompt (`POST /v1/tasks/{id}/prompt`), message (`/message`), status (`PATCH /v1/tasks/{id}`),
kill (`/kill`), exit (`/exit`), shelve and unshelve (via `PATCH`), delete, launch (`POST /v1/launch`), browse
(`GET /v1/browse`), permission decide (`POST /v1/permissions/{id}/decide`), rules add and delete, harness save and
delete, hooks install, settings, overlays, and shutdown. Seventeen surfaces. The mechanical check is that after this
stage **no `api()` call in the page passes fewer than three arguments except the ones that act on the forum
itself**, and that is greppable.

Per-peer backoff belongs here too. Ten atria at a five second poll is 30 requests every five seconds arriving at one
forum. A peer whose last fan-out failed should be polled at 5, 10, 20 and then 30 seconds until it answers, and the
board should say it is being backed off rather than silently skipping it.

**How you know it works.**

- Two machines, one card each, one stack, ordered by who has waited longer. Verify the order is by `wait_seconds`
  and not by peer, by making the second machine's card older.
- Pull the network on one machine. Its cards grey within one poll and stay on screen with an unreachable marker. The
  other keeps updating. Restore it and the marker clears with no reload.
- Approve a permission request raised on the remote atrium. The blocked agent on that machine continues. Verify at
  the agent, not at the board: the board showing "approved" only proves a row was written.
- Shelve a remote card that has a pending request. The request is answered with `shelvedReason`
  (`internal/daemon/daemon.go:428`) and the agent is released.
- Six atria reporting, board open, and every fetch still works. This is the browser connection cap check and it is
  the one that fails without the merged stream.

**Not in this stage.** Attach. Cross-peer alerting. Any caching at the forum.

## Stage 5: attach through the forum

**What it does and why here.** Proves the WebSocket survives the proxy and measures what it costs. Last of the
functional stages because it is the only one that can be lived without, and because it is the one where the answer
might be "this is for reading".

**Files.** Ideally none on the leaf. `internal/daemon/attach.go:47-51` already accepts with
`InsecureSkipVerify: true` and its comment already gives the reason, so no origin check has to be relaxed. If the
stage needs a leaf change, that is a finding worth writing down rather than a step.

**Interfaces.** None new. `httputil.ReverseProxy` carries the upgrade. What this stage adds is a measurement:

```go
// internal/forum/proxy.go, added.

// attachStats is what stage 5 exists to produce. Held in memory, reported on
// /peers, and never written down, for the same reason activity is not.
type attachStats struct {
	Open      int           `json:"open"`
	BytesUp   int64         `json:"bytes_up"`
	BytesDown int64         `json:"bytes_down"`
	// LastRTT is the most recent WebSocket ping round trip through the forum
	// to the leaf. The answer to "is remote attach typeable" is a number.
	LastRTT time.Duration `json:"last_rtt"`
}
```

**How you know it works.** A supervised runner on a remote atrium, attached from the forum's board, shows its ring
buffer backlog on connect (`internal/daemon/attach.go:63-69`) and echoes typed input. Then two checks the design
names and one it does not.

- Measure the round trip and write the number into `docs/backlog.md`. The design says to do this and it is the
  deliverable of the stage.
- Kill the forum with an attach open. The runner keeps running. `attach.go:64` only calls `run.unsubscribe(updates)`
  on the way out and never touches the pty, so the supervisor still owns it. Verify at the leaf's own board by
  attaching again there.
- ConPTY offers no reattach (`docs/architecture-v2.md:634`), so a runner whose pty is torn down is gone. Confirm
  that nothing in the forum path can tear one down: the forum closing a proxied socket must not reach `supervisor`.
  This is the one place where a proxy bug could destroy work rather than inconvenience someone.

**Not in this stage.** Local echo. It would be a lie about a terminal. If the number comes back bad, the answer is
the "open this atrium directly" link the peer view already carries, not a client-side prediction of what the shell
will print.

## Stage 6: alerting across every atrium

**What it does and why here.** The waiting poll, the permission poll, the nag schedule, the title counter and the
desktop notifications cover every reporting atrium rather than the selected one. Last because it depends on every
earlier stage, and mandatory before the forum is described anywhere as a federated board: a permission request
nobody is shown freezes an agent, and that is the failure the project exists to prevent. A forum that shows six
machines and alerts on one is worse than no forum, because it looks like it is watching.

**Files.**

- `internal/api/web/index.html` (changed): the `alerting` IIFE at 3950-4119, `retitle()` at 5128-5131,
  `showNotification()` at 4193-4234, `focusPerm()` at 4415.
- `internal/api/web/sw.js` (changed): the notification click has to carry a peer, or clicking a notification opens
  the wrong machine's card.

**Interfaces.** The change is that every identifier the alerting layer keys on becomes a pair. `knownPerms` and
`knownWaiting` are id sets today, and a permission id from one atrium can collide with one from another because
each leaf mints its own.

```js
// Alerting keys on peer plus id, everywhere. Ids are minted per atrium, so two
// atria can produce the same one, and a naive set would silently swallow the
// second machine's request. That failure is invisible: nothing appears, and
// nothing appearing is exactly what a working board looks like.
const key = (peer, id) => peer + "" + id;
```

`nag(perms)` at 4056-4082 keeps its schedule unchanged, which is 60 seconds of silence, then every minute for ten
minutes, then every five. Its `nagged[p.id]` dedupe becomes `nagged[key(p.peer, p.id)]`. `notify()` at 4027-4034
suppresses when the tab is visible unless `goTo === "perms"`, and that stays, because "visible" is about the tab and
not about which peer is selected.

**How you know it works.** A permission request raised on an atrium the operator is not looking at produces the same
sound, toast and desktop notification as a local one, and clicking through selects that atrium and shows that
request. Then the collision check, which is the one that will actually be wrong: two atria with permission requests
that happen to share an id, both alerting. Force it by seeding the same id in a test rather than waiting for it.

If the fan-out cost has become unpleasant by here, this is where the in-memory cache of the design's section 6(b)
earns its place: the forum already holds one upstream SSE stream per peer from stage 4, so it can hold a last-known
task list beside it, tagged with when it arrived, in memory, never on disk, rendered as stale when it is stale. That
is also what would let the forum drive the nag with no tab open, which nothing else in this design can do. It
remains optional and it remains the only place the forum is allowed to read a payload.

## Stage 7: only if the trust boundary changes, an overlay-native acceptor

**What it does and why here.** Replaces `net.Listen` with a listener the overlay supplies, so the peer name is a
verified fact rather than a claim. Last because it costs a PostgreSQL, an OpenZiti controller, an enrolled edge
router and an agora controller before two processes can speak (`federation-design-v2.md` section 4), and because
answer (i), reachability as authorisation, is correct for one operator's machines.

**Files.** `internal/forum/inbound_agora.go` (new, build-tagged), `internal/cli/forum.go` (changed), and
`docs/overlays.md` (changed, in the same commit).

**Interfaces.** The `Inbound` struct from stage 1 is the whole abstraction and it does not change:

```go
// One tunnel per atrium, not one shared tunnel.
//
// Agora writes a dial policy naming exactly one environment identity per
// attachment, so a listener per atrium means the router already guarantees
// that only that atrium can dial it. Which listener accepted a connection is
// then the identity, --as becomes a label with no authority, and no
// per-connection type assertion to edge.ServiceConn is needed anywhere.
func agoraInbounds(ctx context.Context, ag *agent.Agent, names []string) ([]Inbound, error) {
	var in []Inbound
	for _, n := range names {
		l, err := tunnel.Listen(ctx, ag, "atrium-forum-"+n)
		if err != nil {
			return nil, err
		}
		in = append(in, Inbound{Listener: l, Name: n})
	}
	return in, nil
}
```

When `Inbound.Name` is set, the handshake's `Peer` field is ignored and the routing slot is the listener's name. The
collision table's row three then cannot arise, because two atria cannot accept on one listener.

**How you know it works.** An atrium whose identity is not permitted to dial the forum's service cannot reach it at
all, and the failure appears on the fabric rather than in atrium's logs. An atrium that is permitted appears under
the name its listener carries, and passing a different `--as` does not change which slot it lands in.

**Not in this stage, and stated because it is the temptation.** No shared tunnel with a per-connection identity
read. That path needs a type assertion to `edge.ServiceConn` which agora performs internally and does not export,
and which was verified by reading agora rather than by running it (`federation-design-v2.md` section 11).

**One thing that must land in the same commit.** `docs/overlays.md:13-14` says atrium "never handles an identity"
and that is true today because the tunneler owns the key file. The agora SDK path opens the identity in-process.
That sentence stops being true and has to be rewritten, not quietly outgrown.

## Failure behaviour, checked against the guarantees

The guarantees are in `CLAUDE.md`. Each is checked rather than asserted.

**Storage failure halts, it does not degrade.** The forum has no store, so it cannot halt, and the derivation for
why it must not have one is in `federation-design-v2.md` section 6(c). A leaf that halts closes its own agent
listener and keeps its own board up (`internal/daemon/daemon.go:221-235`). Its forwarded `GET /v1/health` returns
`{"ok":false,"halted":true,"cause":...}` with a `200`, because `health` reports the halt in the body rather than the
status (`internal/api/api.go:200-207`), and the board's `#halted` banner already reads exactly that. Under the forum
the banner becomes per peer. A leaf that cannot be reached at all is not halted, it is unreachable, and the two must
render differently or the operator will restart the wrong thing.

**A hook must never fail a session.** Unaffected, and this must stay true by construction. Hooks find their daemon
through `internal/daemon/whereami.go`, which defaults to `localhost:7777`. No stage in this plan touches that path.
Rule: **no federation feature may put a link between a hook and its daemon.** If a future stage proposes it, that is
the stage to refuse.

**`/activity` is fire and forget.** Unaffected for the same reason.

**Shutdown is narrated and bounded.** `d.shutdown` (`daemon.go:567-636`) releases event streams first, closes
overlays, gives supervised runners ten seconds, then closes both listeners. The link goroutine has to be cancelled
in that sequence, and it belongs beside `d.closeOverlays()` at line 578 and for the same reason: an address that
outlives the board it points at answers with a connection refused. Closing the link makes the leaf drop off the
forum within `peerGrace` rather than sitting there as a peer that times out on every request.

**A kill is not a stop.** Covered by the `sharing()` change in stage 2, which is where the hazard is opened and
therefore where it is closed.

Four more cases the guarantees do not name.

**A leaf vanishes mid-request.** `ReverseProxy` calls its `ErrorHandler`. The forum answers `502` with
`{"error":"...","peer":"laptop","unreachable":true}`, marks the peer unreachable in the table, and lets the pool
drain. The board greys that peer's cards and leaves them on screen. A permission request that was pending on that
leaf stays pending on that leaf, and the agent stays blocked, which is exactly what happens today when a browser tab
closes mid-decision (`internal/hub/hub.go:407` blocks on a channel with no timeout). Same class of failure, same
answer: reopen a surface that can reach it and answer again.

**A leaf reconnects.** New connections arrive with the same `Instance`, join the existing slot, and the next
five second poll succeeds. No page reload, no reconciliation, nothing to resync, because the forum was holding
nothing that could have gone stale. This is the payoff for stage 1's instance rule and it is the property that makes
a rescheduled kubernetes pod a non-event.

**The forum dies.** Every leaf's `Link.Run` sees its connections close, redials on the 5s to 60s backoff shape the
v1 agent already uses, and logs once and then rarely. Every leaf's board, gate, store, ptys and agent listener are
untouched. The operator opens the leaf directly and the gate still answers. This is what makes the forum additive:
at worst it removes a convenience, never a capability.

**Two forums exist.** Nothing breaks and nothing has to be reconciled, because neither forum holds anything either
one could disagree about. A leaf reporting to two forums is two links and two pools. `--forum` is a single flag in
stage 1 and can become repeatable later with no design consequence. The one thing that is not per forum is
`linked()`, which counts any open link, and that is correct: one link is enough to make loopback stop meaning
anything.

## The board's side

**Where the selector lives.** In the header, beside the connection dot at `index.html`'s `#conn` element, because
that is where "what am I looking at" already is. Three modes: one named atrium, all atria merged, and a link that
opens the selected atrium's own board directly using the `Board` field the handshake carried. The third is not
decoration. It is the answer to a bad attach latency measurement in stage 5 and to a dead forum, and having it in
the UI is what keeps the forum from becoming load-bearing.

**What happens to the SSE stream.** One `EventSource` becomes one `EventSource` at the forum, carrying merged
nudges, for the reason in stage 4: the browser's six-connection-per-origin cap makes one stream per peer fail hard
at six atria. The page's existing handling barely changes, because it already treats events as nudges and refetches
(`index.html:5615`). The three delta-carrying events become nudges naming their peer, and the page refetches that
peer's endpoint instead of applying a payload.

**What the terminal attach does.** It gets one hop longer and nothing else. `internal/daemon/attach.go` needs no
change: it already accepts without an origin check and says why (`attach.go:47-51`). The pty stays on the machine
that created it and always will, because ConPTY offers no reattach (`docs/architecture-v2.md:634`). The socket URL
gains the same prefix `api()` gained. The board must not offer attach for an atrium it cannot reach, because a
socket that never opens looks identical to a runner with nothing to say.

## Migration and compatibility

**No schema migration, in any stage.** Forum configuration is a string and a name, and if either is ever persisted
rather than passed as a flag, `internal/store/settings.go:19-45` already stores arbitrary keys in the `setting`
table created by `0018_setting` (`internal/store/schema.go:368`). Nothing in this plan adds a column, a table or a
constraint. That is worth stating loudly because `CLAUDE.md`'s migration section exists due to a migration edited in
place, and the cheapest way not to repeat that is not to write one.

**An atrium that never joins a forum is unaffected, and the mechanism is that the code does not run.** `Options.Forum`
defaults to empty, `d.link` is nil, the link goroutine never starts, `linked()` returns false at its first line, and
`sharing()` behaves exactly as it does today. There is no branch on any hot path: not in `onPermRequest`, not in
`handleActivity`, not in `Register`, not in the reaper. The one behaviour that changes for an atrium that **does**
open a link is the shutdown endpoint, and that change is the point.

**Forward compatibility across versions.** `Hello.Proto` is `1`. A forum that reads a proto it does not know refuses
with a reason naming both versions, which is a one-line log on both sides rather than a connection that half works.
`Hello.Version` carries the leaf's build so a mismatch is diagnosable from `/peers` without asking anyone.

**The board asset is served from one place.** `internal/api/web.go` already embeds it and the forum serves the same
`http.Handler`. A copied file would drift, and the drift would appear as a feature that works locally and not
through the forum, which is the worst shape a bug can have.

**One doc obligation per stage, not one at the end.** `CHANGELOG.md` gets an entry for the shutdown behaviour change
in stage 2 specifically, because it is the only stage that changes what an existing single-machine atrium does.
`docs/backlog.md` gets the attach latency number from stage 5. `CLAUDE.md`'s subcommand table gets a line for
`atrium forum` in stage 1, next to `atrium hub`, saying they have nothing to do with each other.

## Where the design turned out to be under-specified

Five places. Each one is a decision this plan had to make that the design did not make.

**The browser connection cap.** The design puts the forum's SSE handling in stage 6 as an optional cache
optimisation. Six atria, one origin, one `EventSource` each, and the board stops working entirely, because Chrome
and Firefox cap HTTP/1.1 connections per origin at six. This is a hard failure at the exact scale the feature is
for. This plan moves a merged nudge stream into stage 4 and constrains it so the forum reads SSE framing and never a
payload, which keeps rule 6.

**Two atria claiming one peer name.** The design covers `wire_name` collisions inside a leaf and correctly rules
them orthogonal, but says nothing about two leaves claiming one `--as`. The forum has to have an answer at the
handshake or a restarting daemon locks itself out. This plan adds an `Instance` to the handshake and writes out the
four cases.

**`RemoteAddr` on a dialled connection.** The design says a forum link is the same hazard as a share. It is worse
in one specific way it does not name: when the forum runs on the same machine, every forwarded request presents as
`127.0.0.1` to `isLoopback` (`internal/daemon/shutdown.go:51-58`), which is the ordinary development setup. That
makes the stage 2 `sharing()` fix load-bearing rather than tidy, and it means the refusal message needs a second
wording, because "a share is running" is false when the cause is a link.

**The pool's steady state.** The design lists connection lifetime under an idle SSE stream and how many spares to
hold as unknowns. This plan answers with a mechanism, first-byte-triggered refill, and two numbers with defaults,
`minSpare` 4 and `maxConns` 32, and then reports the live count on `/peers` so the numbers can be corrected by
observation rather than by argument. It remains unprototyped.

**Per-peer alerting keys.** The design says stage 6 makes alerting cover every atrium. It does not say that
`knownPerms` and `knownWaiting` (`index.html:4087-4118`) are id sets and that ids are minted per leaf, so two atria
can produce the same one and the second machine's request is silently swallowed. That failure is invisible: nothing
appears, which is what a working board also looks like. Every alerting key becomes a peer-plus-id pair.

Two things this plan could not verify by reading and that are the first things to check when building.

`httputil.ReverseProxy` carrying a `101 Switching Protocols` upgrade over a `Transport` whose `DialContext` returns
a pooled connection is documented stdlib behaviour and was not exercised. Stage 5 is the proof, and if it fails the
fallback is a hand-written hijack-and-copy in `internal/forum/proxy.go`, which is maybe forty lines and does not
change any interface here.

Attach latency through two hops remains a prediction. "For reading, not typing" is what the design expects and what
this plan assumes when it puts the direct-to-leaf link in the peer selector. Stage 5 turns it into a number.
