package conn

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/link/transport"
	"github.com/dovholuknf/atrium/internal/link/wire"

	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub's half: accept what rooms dial, and lend those connections out as if
// the hub had dialled them.
//
// THE HUB CONNECTIONS HOLD NOTHING DURABLE. No card, no scrollback. Kill it
// mid-sentence and the only thing lost is the socket. That is not tidiness, it
// is the entire point: the hub is the part being restarted every few minutes
// while somebody changes CSS, so it must never be the part that owns anything
// worth keeping.

// Hub accepts rooms and proxies to them.
type Hub struct {
	// peerKey, when set, replaces the package `peerKey` for this hub. A seam for tests.
	peerKey func(net.Conn) string
	T       Timings
	// lst is this hub's own sides, for health and for telling `unlistened` from `none`. See pathstate.go.
	lst listenerSet
	// Enrol answers a room that is joining for the first time and has no
	// certificate yet. Supplied by the transport, because what a credential IS
	// is the transport's business and not this file's. Nil refuses enrolment.
	// Over ziti and zrok it is set too, and take() lets it run only inside
	// TLS, so a room joining with a new-form string is named by the
	// certificate it leaves with. An old-form room never calls it.
	Enrol func(net.Conn, *bufio.Reader) (string, error)
	// Authenticated reports whether a connection proved who it is. A transport
	// that carries identity itself answers true. Nil means "yes", which is
	// right for a test over a pipe and nowhere else.
	Authenticated func(net.Conn) bool
	// LegacyRefused says whether the hub has switched off the old, certificate-less
	// path on an overlay (the `overlay_legacy` setting is `refuse`). Asked on every
	// connection rather than read once, so flipping the setting takes effect at the
	// next attach without a restart. Nil means allowed, which is what every
	// transport but an overlay is, since only an overlay has an old path.
	LegacyRefused func() bool
	// OnUnproven is told, once per attach, that a room attached without a
	// certificate. It is how the operator learns who must re-join before the old
	// path is switched off. Nil on a hub that records nothing.
	OnUnproven func(name, transport string)
	// Enrolled says whether a room by this name has ever proved itself with a
	// certificate. A connection on the old path under such a name is refused,
	// attached or not: the name is proven, and a peer the overlay merely admits
	// must not be able to wear it. Nil means no name is, which is a hub with no
	// store. See f-026.
	Enrolled func(name string) bool
	// Proved is told that a room attached with a certificate, so a name proven
	// before its enrolment was written down is written down now. Nil records
	// nothing.
	Proved func(name string)
	// OnUnprovenRefused is told that a connection on the old path claimed a
	// proven name and was turned away, and why. At most once per name per
	// `refusedEvery`, because whoever it is will redial on a backoff. Nil records
	// nothing.
	OnUnprovenRefused func(name, transport, why string)
	// Attaching is asked before a room is adopted, and may refuse it.
	//
	// TWO THINGS AT ONCE, and they are the same thing seen from both ends.
	// It is where a hub checks that the room dialling in is one it has a record
	// of, and it is where the observed half of that record gets written: what
	// the machine calls itself and what it is running.
	//
	// A CERTIFICATE IS NOT A RECORD. A room whose row was forced out still
	// holds papers this hub signed, and would otherwise attach to a hub that
	// has forgotten it exists: not on the list, nothing to name it, and no way
	// to mark it for deletion again. The error is shown to the room, so it must
	// read as an instruction rather than as a refusal.
	//
	// Nil accepts everything, which is right for a test over a pipe and for a
	// hub that has no store.
	Attaching func(name, host, version string) error
	// Cached is handed everything a room says it is holding, whole.
	//
	// Nil means this hub keeps no cache, which is what a hub with no store is,
	// and a room that announces to one is told so rather than left waiting.
	// See announce.go.
	Cached func(name string, cards []CardState) error

	// OnAttach is called ONCE when a room adopts, and OnDetach ONCE when the hub
	// lets it go. They are the operational audit's attach and detach lines. Both
	// nil on a hub that records nothing, and both are best effort: they must not
	// block the link. `Attaching` is the wrong place for this, because it is
	// asked again on every heartbeat and would record an attach a second.
	OnAttach func(name, host, version string)
	OnDetach func(name, why string)

	// Relay answers a room asking for something to be carried to another room:
	// a message, or the list of who is there. `from` is the asking room, from
	// its certificate. Nil refuses, which is right for a hub with no board to
	// reach the other rooms through. See relay.go.
	Relay func(ctx context.Context, from string, req wire.RelayRequest) RelayAnswer

	// Git serves the hub's repositories, as plain HTTP, to a room that dialled the `git`
	// kind. Nil means this hub does not say Git in its welcome and refuses the kind. It is
	// served on that kind and nowhere else, never on the board listener. See git.go.
	Git http.Handler
	// GitStore serves the hub's own store (`/git/hub/...`) to a room on the same `git` kind, as that room.
	// Nil serves rooms the mirrors alone. The board serves the same store to the operator. See git_store.go.
	GitStore http.Handler
	// GitLookup answers a room's atrium_git_url on the same `git` kind, at gitsync.LinkLookupPath, with the URLs on
	// gitsync.LinkBase. Nil answers 404, which a room reads as a hub older than it and reads the store alone.
	GitLookup func(ctx context.Context, q gitsync.URLQuery) gitsync.URLAnswer
	// PRClaim answers a room's claim on a PR key, on the same `git` kind connection, at PRClaimPrefix. The room is the
	// one the hello named, set on the request by serveGit and never read from the room's own headers. Nil answers 404,
	// which a room reads as a hub that cannot be asked, so its row is `claim: pending`. See prclaim.go.
	PRClaim http.Handler
	// RoomSpec answers a room's read of its own spec and post of its lock, on the `roomspec` kind, as the room the
	// certificate named, which serveRoomSpec sets in RoomSpecRoomHeader. Nil refuses the kind. See roomspec.go.
	RoomSpec http.Handler
	// Forge answers a room's forge question (a pull request, an issue, a repository to hold) on the same `git` kind
	// connection, at ForgePrefix. The room is the one the hello named, set by serveGit. Nil answers 404, which a room
	// reads as a hub that cannot be asked. See forgeroute.go.
	Forge http.Handler
	// Paths are the transports this hub listens on, in the order rooms should
	// try them, said in every welcome. Asked at each attach, so a change to the
	// order reaches the next room that attaches. Nil says nothing, and a room
	// keeps what it last heard.
	Paths func() []Path
	// OnStream records an ssh stream's open, close or refusal as an audit line under the target room. Nil records
	// nothing. NewProxy wires it to the hub's audit log. See stream.go.
	OnStream func(room, kind, detail string)
	// LastSeen says when an offline room was last heard from, for the sentence a refused stream gets. Nil says nothing.
	LastSeen func(room string) (time.Time, bool)
	// OnLogin hears a room say its claude login needs doing, once per spell: only the edge from fine to needed, never
	// the beats that repeat it. Nil does nothing. NewProxy wires it to the notifier.
	OnLogin func(room, reason string)

	mu    sync.Mutex
	rooms map[string]*attached
	// refused is when an old-path claim on a proven name was last reported, by
	// folded name. See `OnUnprovenRefused`.
	refused map[string]time.Time
	// builds are the binaries this hub can hand out, one per platform. Empty
	// means it offers nothing, which is the default until `Offers` is called.
	// See `upgrade.go`.
	builds []Build

	// changed is the one hook the event feed waits on: a room attached, or a room
	// went. A buffered channel of ONE with a non-blocking send, so a burst
	// coalesces into a single wake and a wake is never lost. Nobody is required to
	// be listening: a hub with no board open leaves the token sitting there, and
	// the next reconciler starts by looking at the truth anyway.
	changed chan struct{}
	// Every is the everywhere index, nil on a hub that keeps none. See everywhere.go.
	Every Index
}

// Changes is the channel that fires when the set of attached rooms may have
// changed, from attach and from `forget`, which every eviction path goes through
// (hung up, missed beats, listener stopped, record gone).
//
// A WAKE, NOT A DELTA. It says "look again", never what changed, because the
// reader has to compare against what it last saw in any case, and a channel of
// one cannot carry a list.
func (h *Hub) Changes() <-chan struct{} { return h.changed }

// changedNow tells whoever is listening, without ever waiting for them.
func (h *Hub) changedNow() {
	select {
	case h.changed <- struct{}{}:
	default:
	}
}

// loginChanged is a room saying its claude login needs doing, or no longer does. The board is told at once, and
// OnLogin hears the edge so it can ring once: a beat repeating the same reason never gets here.
func (h *Hub) loginChanged(room, was, now string) {
	log.Printf("[hub] %q claude login: %q -> %q", room, was, now)
	h.changedNow()
	if now != "" && was == "" && h.OnLogin != nil {
		h.OnLogin(room, now)
	}
}

// Offers tells rooms what binaries this hub has.
//
// SAYING, NOT DOING. A hub can describe a build and serve it to a room that
// asks for it. It can never install one: the room decides whether it wants it,
// fetches it itself, and checks what arrived. See `upgrade.go` for why it is
// this way round.
//
// ONE PER PLATFORM, because rooms are on other machines and that is the point
// of them. A hub holding only its own binary is useful to a fleet that matches
// it and useless to any other, which would make this feature do nothing for
// exactly the people with several kinds of machine.
func (h *Hub) Offers(builds ...Build) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.builds = builds
}

// attached is one room's live link.
type attached struct {
	name    string
	version string
	host    string
	// commit is the commit the room said it was built from, in its hello. Observed.
	commit string
	// os and arch are what the room said it runs on, in its hello. Observed, and
	// they ride the `rooms` event so the board can say which build a machine wants.
	os, arch string
	// tools is where the room said each toolchain is, in its hello. Observed.
	tools map[string]string
	// unproven is the overlay a room attached over WITHOUT a certificate, so its
	// name is only what it said. Empty for a room the hub's certificate names.
	unproven string
	session  string
	// git is whether this room said, in its hello, that it takes git syncs.
	git bool
	// local is a room that attached from the hub's own machine: a loopback address, or the hub's host name.
	local bool
	// via is the transport whose listener took the control connection, the hub's
	// own observation. fallback is the room's word that it is not its first choice.
	via      string
	fallback bool
	since    time.Time
	// key identifies the credential this room attached with, so a reconnect
	// can be told from somebody else claiming the same name. Empty on a
	// transport that carries no key of its own, where the network has already
	// decided and there is nothing here to defend.
	key string

	// control is the connection the room dialled first, and the only one that
	// stays framed. Writing to it asks for more data connections.
	control net.Conn
	// wmu serialises every write on control, and `control` holds it from the
	// moment this room is published until the welcome is written. THE WELCOME
	// MUST BE THE FIRST FRAME. Publishing fires the attach hooks, and anything
	// they start that wants a connection writes `need` here. Written first,
	// that `need` is read by the room as a welcome with no `ok` in it, which it
	// reports as a refusal with no reason, and it redials. On 2026-09-29 that
	// kept the live rooms off the hub for two and five minutes after a restart.
	// Also keeps two writers from interleaving the bytes of one frame.
	wmu sync.Mutex
	// idle holds data connections nobody is using. Buffered generously: a room
	// answering a burst of `need` all at once must not block on handing them
	// over.
	idle chan net.Conn
	// want is how many spare connections to keep. Grows with load.
	want int
	// access is what the room last said about its remote access switch, nil until it says. Observed, in memory only.
	// streams is how many ssh streams the hub has open to it. Both under mu.
	access  *RemoteAccess
	streams int
	// login is why the room last said its claude login needs doing, "" when fine. Under mu.
	login string
	// paths is what the room last said about each rung, on its beat. Under mu. Nil from a room that never says.
	paths []PathState

	lastBeat time.Time
	// lastData is when the last data connection was filed into the pool, and
	// starvedSaid whether the hub has logged that none is arriving. See starved.
	lastData    time.Time
	starvedSaid bool
	// idleCPU is the idle percent the room last said on a beat, valid when haveIdle.
	idleCPU  float64
	haveIdle bool
	mu       sync.Mutex
	closed   bool
	done     chan struct{}
}

// newHub makes an empty hub with no everywhere index.
func newHub(t Timings) *Hub {
	return &Hub{T: t.Fill(), rooms: map[string]*attached{}, changed: make(chan struct{}, 1)}
}

// Serve accepts connections until the listener closes.
//
// The listener is whatever the transport handed over: a TLS listener today, a
// ziti or zrok one later. This function does not know and must not learn.
func (h *Hub) Serve(ctx context.Context, ln net.Listener) error { return h.ServeVia(ctx, ln, "") }

// ServeVia is Serve for one of several listeners, naming the transport it is, so
// the board can say which one each room came in on. A hub may serve any number
// at once. See docs/rnd/room-transport-order.md.
func (h *Hub) ServeVia(ctx context.Context, ln net.Listener, via string) error {
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
		go h.take(ctx, conn, via)
	}
}

// take handles one freshly accepted connection, whichever kind it is.
func (h *Hub) take(ctx context.Context, conn net.Conn, via string) {
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
		// A CONNECTION WITHOUT TLS CANNOT ENROL. The secret is spent inside the
		// pinned handshake, which is what proves the hub to the room, so the old
		// path is never a way to hand a secret over.
		if _, legacy := transport.LegacyTransport(conn); legacy {
			_ = writeJSON(conn, welcome{OK: false,
				Error: "joining has to run inside TLS. update this room to a build that has it"})
			return
		}
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
			Error: "this connection presented no credential. run `atrium room join` first"})
		conn.Close()
		return
	}

	// THE OLD PATH IS ALLOWED UNTIL THE OPERATOR SAYS OTHERWISE. A room that
	// joined an overlay before certificates reached it dials without one, and
	// turning it away on upgrade would be an outage nobody chose. The switch is a
	// hub setting, never flipped by anything but a person.
	if _, legacy := transport.LegacyTransport(conn); legacy && h.LegacyRefused != nil && h.LegacyRefused() {
		_ = writeJSON(conn, welcome{OK: false, Error: RejoinSentence(hi.Room)})
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
		if name != "" && !wire.EqualFold(who, name) {
			log.Printf("[hub] %q presented a certificate for %q, using the certificate", name, who)
		}
		name = who
	}
	if name == "" {
		_ = writeJSON(conn, welcome{OK: false, Error: "this room has no name"})
		conn.Close()
		return
	}

	// A PROVEN NAME IS NOT TAKEN ON THE OVERLAY'S WORD, for any kind. On the old
	// path the name is only what the hello said, so without this any peer the
	// overlay admits could replace a room with a certificate, or relay and
	// announce as it. Refused whether or not that room is attached right now.
	// A room that never enrolled keeps attaching as it always did.
	if over, legacy := transport.LegacyTransport(conn); legacy {
		if why := h.provenName(name); why != "" {
			h.refuseUnproven(name, over, why)
			_ = writeJSON(conn, welcome{OK: false, Error: "the name " + name + " belongs to a room " +
				"with a certificate, so it cannot attach without one. " + RejoinSentence(name)})
			conn.Close()
			return
		}
	}

	switch hi.Kind {
	case "control":
		h.control(ctx, name, hi, conn, br, via)
	case "data":
		h.data(name, hi, conn, br, via)
	case announceKind:
		// A ROOM SAYING WHAT IT IS HOLDING. Its own connection rather than a
		// line on the control one, because a card list is not small. See
		// `announce.go`.
		defer conn.Close()
		h.serveAnnouncement(name, conn, br)
	case upgradeKind:
		// A ROOM ASKING FOR THE BINARY IT WAS OFFERED. It dialled this, which
		// is the whole design: the hub never reaches into a room. See
		// `upgrade.go`.
		log.Printf("[hub] room %q is taking a %s/%s build", name, hi.OS, hi.Arch)
		h.serveUpgrade(conn, hi)
	case relayKind:
		// A ROOM ASKING FOR SOMETHING TO BE CARRIED TO ANOTHER ROOM. Dialled by
		// the room, answered once, closed. See `relay.go`.
		defer conn.Close()
		h.serveRelay(ctx, name, conn, br)
	case gitKind:
		// A ROOM FETCHING A REPOSITORY, as HTTP on this connection. Dialled by the room
		// after the hub said Git. See `git.go`.
		defer conn.Close()
		h.serveGit(name, hi.Session, conn, br)
	case streamKind:
		// A ROOM ASKING FOR AN SSH STREAM TO ANOTHER ROOM'S SSHD. Dialled by the room, named by its certificate, then
		// raw bytes. See stream.go.
		defer conn.Close()
		h.serveStream(name, conn, br)
	case roomSpecKind:
		// CERTIFICATE ONLY. Every other kind falls back to the name in the hello for a connection that has no
		// certificate to read, which is right for them and wrong for a spec: a peer the overlay admits could name a
		// room that never enrolled and read or write its spec and lock. A connection with nothing for `identify` to
		// read is turned away here, whether it is the old overlay path or anything else that is not a certificate.
		// With h.Authenticated nil (no mutual TLS) the hub trusts every hello for every kind, this one included.
		if h.Authenticated != nil && identify(conn) == "" {
			_ = writeJSON(conn, welcome{OK: false, Error: "a room's spec is served only to a room that proves its name " +
				"with a certificate. " + RejoinSentence(name)})
			conn.Close()
			return
		}
		// A ROOM ASKING FOR ITS OWN SPEC, or leaving its lock, as HTTP on this connection. See roomspec.go.
		defer conn.Close()
		h.serveRoomSpec(name, conn, br)
	}
}

// control adopts a room and holds its control connection.
func (h *Hub) control(ctx context.Context, name string, hi hello, conn net.Conn, br *bufio.Reader, via string) {
	// ASKED BEFORE ANYTHING IS ADOPTED, so a room the hub has no record of is
	// turned away rather than half joined. Refusing later would mean it is in
	// the map, being proxied to, while the hub says it does not exist.
	if h.Attaching != nil {
		if err := h.Attaching(name, hi.Host, hi.Version); err != nil {
			log.Printf("[hub] refused %q: %v", name, err)
			_ = writeJSON(conn, welcome{OK: false, Error: err.Error()})
			conn.Close()
			return
		}
	}

	session := newSession()
	a := &attached{
		name: name, version: hi.Version, host: hi.Host, commit: hi.Commit, session: session,
		os: hi.OS, arch: hi.Arch, git: hi.Git, tools: hi.Tools,
		since: time.Now(), control: conn,
		unproven: unprovenOver(conn),
		local:    localConn(conn, hi.Host),
		via:      via, fallback: hi.Fallback,
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
	a.key = h.keyOf(conn)
	// Held until the welcome is written, below. See `wmu`.
	a.wmu.Lock()
	welcomed := false
	defer func() {
		if !welcomed {
			a.wmu.Unlock()
		}
	}()
	h.mu.Lock()
	old, taken := h.rooms[wire.KeyOf(name)]
	// A KEYLESS DIAL CANNOT REPLACE A KEYED ROOM either: it has nothing to show
	// that it is the same room. Before f-026 an empty key slipped past this.
	if taken && old.key != "" && old.key != a.key {
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
	h.rooms[wire.KeyOf(name)] = a
	h.mu.Unlock()
	// A RECONNECT WITH A NEW VERSION COMES THROUGH HERE TOO, which is why this is
	// the one place attach fires from: the fingerprint downstream decides whether
	// anything a board draws actually moved.
	h.changedNow()

	// ONCE, HERE, not in `Attaching`, which is asked again every heartbeat. A
	// reconnect comes through this path too and is worth a line: a hub restart
	// makes every room reattach, and the burst is the record of the restart from
	// the rooms' side.
	if h.OnAttach != nil {
		h.OnAttach(name, hi.Host, hi.Version)
	}
	if a.unproven != "" && h.OnUnproven != nil {
		h.OnUnproven(name, a.unproven)
	}
	if a.key != "" && h.Proved != nil {
		h.Proved(name)
	}

	log.Printf("[hub] room %q attached from %s", name, conn.RemoteAddr())
	// Bounded, because a newer connection's `old.close` waits on `wmu` while it
	// holds `h.mu`, and a room that stopped reading must not freeze every attach.
	_ = conn.SetWriteDeadline(time.Now().Add(handshakeWait))
	err := writeJSON(conn, welcome{
		OK: true, Session: session, Warm: h.T.Warm, Caches: h.Cached != nil,
		Git: h.Git != nil, Paths: h.paths(),
	})
	_ = conn.SetWriteDeadline(time.Time{})
	welcomed = true
	a.wmu.Unlock()
	if err != nil {
		// Forgotten as well as closed. Nothing watches a room that was never
		// welcomed, so it would otherwise stay listed until it reconnected.
		a.close(err.Error())
		h.forget(name, a, err.Error())
		return
	}

	go h.watch(ctx, a)

	// WHAT THIS HUB IS RUNNING, said once, to a room that asked to be told.
	//
	// Said and then forgotten: the hub does not follow it up, does not retry,
	// and never finds out what the room decided. A room that wants it dials
	// for it. See `upgrade.go`.
	h.mu.Lock()
	builds := h.builds
	h.mu.Unlock()
	if b := forRoom(builds, hi); b != nil {
		log.Printf("[hub] telling %q about %s for %s/%s, which it may take or ignore",
			name, b.Version, b.OS, b.Arch)
		offer := b.Offer
		_ = a.send(note{Offer: &offer})
	}

	for {
		var n note
		if err := readJSON(br, &n); err != nil {
			break
		}
		if n.Access != nil {
			acc := *n.Access
			a.mu.Lock()
			a.access = &acc
			a.mu.Unlock()
			h.changedNow()
		}
		if n.Beat > 0 {
			a.mu.Lock()
			a.lastBeat = time.Now()
			if n.IdleCPU != nil && *n.IdleCPU >= 0 && *n.IdleCPU <= 100 {
				a.idleCPU, a.haveIdle = *n.IdleCPU, true
			}
			was := a.login
			if n.Login != nil {
				a.login = *n.Login
			}
			now := a.login
			if n.PathStates != nil {
				a.paths = cleanPaths(n.PathStates)
			}
			a.mu.Unlock()
			if n.Login != nil && now != was {
				h.loginChanged(name, was, now)
			}
			// Echoed, so the room can tell a live socket from a half-open one.
			// A write that succeeds into a dead connection is the failure mode
			// a heartbeat exists to catch, and only a round trip catches it.
			_ = a.send(note{Beat: n.Beat})
		}
	}

	a.close("the room hung up")
	h.forget(name, a, "the room hung up")
}

// watch evicts a room that has stopped beating.
func (h *Hub) watch(ctx context.Context, a *attached) {
	t := time.NewTicker(h.T.Beat)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			// THE ROOM GOES WITH IT, rather than being left in the list.
			//
			// For a hub being shut down this changes nothing: the process is
			// going. It matters for a listener that stops while the hub keeps
			// running, which is what a hub that is also a room does every time
			// somebody turns that off. Returning quietly left a room listed,
			// heartbeat frozen, forever: nothing was watching it any more, so
			// nothing would ever notice it had gone.
			a.close("its listener stopped")
			h.forget(a.name, a, "its listener stopped")
			return
		case <-a.done:
			return
		case <-t.C:
			// ASKED AGAIN, EVERY BEAT, and not only when a room first attaches.
			//
			// A room's record can go while it is attached: the hub's store is a
			// file, and `atrium2 hub room rm --force` is another process
			// writing to it. Checking only at attach would leave the hub
			// proxying to a room it no longer has any record of, which is the
			// one state nothing else in the design knows how to describe.
			//
			// It is also what keeps "last heard from" true rather than a
			// timestamp from whenever the link happened to be established, so
			// anything asking whether a room is live has something honest to
			// read.
			if h.Attaching != nil {
				if err := h.Attaching(a.name, a.host, a.version); err != nil {
					log.Printf("[hub] letting %q go: %v", a.name, err)
					a.close("this hub no longer has a record of this room")
					h.forget(a.name, a, "this hub no longer has a record of this room")
					return
				}
			}
			a.mu.Lock()
			quiet := time.Since(a.lastBeat)
			a.mu.Unlock()
			if quiet > h.T.Silence {
				log.Printf("[hub] room %q went quiet for %s", a.name, quiet.Round(time.Second))
				a.close("no heartbeat")
				h.forget(a.name, a, "no heartbeat")
				return
			}
			// SAID ON THE CHANGE, both ways, and the board told. A room whose
			// control connection is up while no data connection arrives beats on
			// time and serves nothing, and it must not read as attached.
			starved := h.starved(a, time.Now())
			a.mu.Lock()
			changed := starved != a.starvedSaid
			a.starvedSaid = starved
			a.mu.Unlock()
			if changed {
				if starved {
					log.Printf("[hub] room %q is attached but no data connection has arrived in %s. "+
						"the board cannot reach it. its data dials are failing", a.name, h.T.Starve)
				} else {
					log.Printf("[hub] room %q: data connections are arriving again", a.name)
				}
				h.changedNow()
			}
		}
	}
}

// starved is a room whose pool is empty and has had no data connection filed
// for Starve, since it attached or since the last one. The hub asks for more on
// every request that finds the pool short, so a pool that stays empty is a room
// whose data dials are not getting here.
func (h *Hub) starved(a *attached, now time.Time) bool {
	if len(a.idle) > 0 {
		return false
	}
	a.mu.Lock()
	last := a.since
	if a.lastData.After(last) {
		last = a.lastData
	}
	a.mu.Unlock()
	return now.Sub(last) > h.T.Starve
}

// data files a fresh connection into a room's pool.
func (h *Hub) data(name string, hi hello, conn net.Conn, br *bufio.Reader, via string) {
	h.mu.Lock()
	a := h.rooms[wire.KeyOf(name)]
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
	a.mu.Lock()
	a.lastData = time.Now()
	a.mu.Unlock()
	select {
	case a.idle <- &buffered{Conn: conn, br: br, via: via}:
	default:
		// More than the pool wants. Closed rather than queued: a connection
		// nobody will use is a file handle on both machines.
		conn.Close()
	}
}

// forget drops a room from the live map, and records the detach when it was
// this connection that was dropped.
//
// `why` is the same reason handed to `a.close`, so the audit line reads as the
// log line does. OnDetach fires only inside the `cur == a` branch, so a stale
// connection losing a race to a reconnect does not record a spurious detach.
func (h *Hub) forget(name string, a *attached, why string) {
	h.mu.Lock()
	removed := false
	if cur, ok := h.rooms[wire.KeyOf(name)]; ok && cur == a {
		delete(h.rooms, wire.KeyOf(name))
		removed = true
	}
	h.mu.Unlock()
	if removed {
		h.changedNow()
	}
	if removed && h.OnDetach != nil {
		h.OnDetach(name, why)
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

	_ = a.send(note{Bye: why})
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
	a := h.rooms[wire.KeyOf(room)]
	h.mu.Unlock()
	if a == nil {
		return nil, ErrNoRoom
	}

	// Take a pooled connection when there is one, and top the pool back up. The
	// top-up is what keeps the next request from paying the dial latency.
	//
	// Only when the pool is actually short, though. Asking on every request
	// made the room dial a fresh connection per board request under load, most
	// of which arrived to a full pool and were closed on sight: a whole
	// handshake per request, for nothing.
	select {
	case c := <-a.idle:
		if len(a.idle) < a.want {
			a.request(1)
		}
		return c, nil
	default:
	}

	// THE POOL WAS EMPTY, so this call is a waiter and nobody has asked for its
	// connection. Deciding to ask from len(a.idle) BEFORE taking one is a race:
	// twelve dials that all see a full pool ask for nothing, a few take what is
	// there and the rest wait the whole DialWait for a connection no one was
	// told to dial. A waiter always asks for its own.
	a.request(1)

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

// AskRestart forwards a restart instruction to an attached room.
//
// THE HUB SPAWNS NOTHING. It writes one line on the room's control connection
// and is done: the room parks its own agents, spawns its own detached restarter
// and winds itself down. A room that is not attached cannot be asked, which is
// the honest answer rather than a queued intention against a machine nobody has
// heard from.
func (h *Hub) AskRestart(room string, ask RestartAsk) error {
	h.mu.Lock()
	a := h.rooms[wire.KeyOf(room)]
	h.mu.Unlock()
	if a == nil {
		return ErrNoRoom
	}
	a.mu.Lock()
	closed := a.closed
	a.mu.Unlock()
	if closed {
		return ErrNoRoom
	}
	if err := a.send(note{Restart: &ask}); err != nil {
		return fmt.Errorf("could not reach the room over the link: %w", err)
	}
	return nil
}

// PushPaths tells every attached room the hub's list of paths as it is now. Called when it changed, since
// a welcome carries the list only to a room that attaches. Best effort: a room it cannot reach is gone, and
// its next welcome carries the list anyway.
func (h *Hub) PushPaths() {
	ps := h.paths()
	if len(ps) == 0 {
		return
	}
	h.mu.Lock()
	live := make([]*attached, 0, len(h.rooms))
	for _, a := range h.rooms {
		live = append(live, a)
	}
	h.mu.Unlock()
	for _, a := range live {
		_ = a.send(note{Paths: ps})
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
	_ = a.send(note{Need: n})
}

// send writes one frame on the control connection, after the welcome and never
// through the middle of another frame. See `wmu`.
func (a *attached) send(v any) error {
	a.wmu.Lock()
	defer a.wmu.Unlock()
	return writeJSON(a.control, v)
}

// dialer is `Dial` bound to one room, in the shape `http.Transport` wants.
func (h *Hub) dialer(room string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, _, _ string) (net.Conn, error) {
		return h.Dial(ctx, room)
	}
}

// unprovenOver names the overlay a connection took the old path on, or "".
func unprovenOver(c net.Conn) string {
	t, _ := transport.LegacyTransport(c)
	return t
}

// refusedEvery bounds how often an old-path claim on one proven name is
// reported. Whoever it is redials on a backoff, and a line a minute would push
// the week's history out of an audit log that keeps a window.
const refusedEvery = 10 * time.Minute

// provenName says why a name cannot be used without a certificate, or nothing.
func (h *Hub) provenName(name string) string {
	h.mu.Lock()
	a := h.rooms[wire.KeyOf(name)]
	h.mu.Unlock()
	if a != nil && a.key != "" {
		return "a room with a certificate is attached under that name"
	}
	if h.Enrolled != nil && h.Enrolled(name) {
		return "that name enrolled a certificate"
	}
	return ""
}

// refuseUnproven logs and reports a refused old-path claim, rate limited.
func (h *Hub) refuseUnproven(name, over, why string) {
	k := wire.KeyOf(name)
	h.mu.Lock()
	if h.refused == nil {
		h.refused = map[string]time.Time{}
	}
	last, seen := h.refused[k]
	quiet := seen && time.Since(last) < refusedEvery
	if !quiet {
		h.refused[k] = time.Now()
	}
	h.mu.Unlock()
	if quiet {
		return
	}
	log.Printf("[hub] refused %q over %s without a certificate: %s", name, over, why)
	if h.OnUnprovenRefused != nil {
		h.OnUnprovenRefused(name, over, why)
	}
}

// Rooms lists what is attached, for the hub's own status endpoint.
func (h *Hub) Rooms() []wire.Attached {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make([]wire.Attached, 0, len(h.rooms))
	now := time.Now()
	for _, a := range h.rooms {
		starved := h.starved(a, now)
		a.mu.Lock()
		beat := a.lastBeat
		var idle *float64
		if a.haveIdle {
			v := a.idleCPU
			idle = &v
		}
		var acc *RemoteAccess
		if a.access != nil {
			v := *a.access
			acc = &v
		}
		login := a.login
		paths := append([]PathState(nil), a.paths...)
		a.mu.Unlock()
		out = append(out, wire.Attached{
			Name: a.name, Version: a.version, Host: a.host, Commit: a.commit,
			Since: a.since, Idle: len(a.idle), Beat: beat,
			OS: a.os, Arch: a.arch, Proven: a.unproven == "", Git: a.git, Local: a.local, IdleCPU: idle,
			Via: a.via, Fallback: a.fallback, Starved: starved, RemoteAccess: acc,
			LoginNeeded: login,
			Paths:       paths,
			Tools:       a.tools,
		})
	}
	return out
}

// Has reports whether a room is attached right now.
func (h *Hub) Has(room string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	_, ok := h.rooms[wire.KeyOf(room)]
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
// can be distinguished from an impersonation.
var peerKey = transport.PeerKey

// keyOf is the key a connection presented, read through the hub's own `peerKey` when a test set one.
func (h *Hub) keyOf(c net.Conn) string {
	if h.peerKey != nil {
		return h.peerKey(c)
	}
	return peerKey(c)
}

// identify asks the transport who authenticated, as a name. A transport with no identity answers empty and the
// hello's claim is used, which is right for a test over a pipe and wrong everywhere else.
var identify = transport.Identify

func newSession() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// keyOf folds a room name, because a room typed with different capitals on two
// machines is one room.

func lowerASCII(s string) string { return wire.LowerASCII(s) }

var _ = http.StatusOK

// paths is what the welcome says about how this hub can be reached.
func (h *Hub) paths() []Path {
	if h.Paths == nil {
		return nil
	}
	return h.Paths()
}

// localConn is whether a room's link came from the hub's own machine.
func localConn(c net.Conn, host string) bool {
	if a := c.RemoteAddr(); a != nil {
		h, _, err := net.SplitHostPort(a.String())
		if err != nil {
			h = a.String()
		}
		if ip := net.ParseIP(h); ip != nil && ip.IsLoopback() {
			return true
		}
	}
	me, err := os.Hostname()
	return err == nil && host != "" && strings.EqualFold(strings.TrimSpace(host), me)
}

// OnlyVia names the attached rooms that reach this hub over a kind and have no other path that answers. A room that has
// reported no paths is counted, since nothing says it has another.
func (h *Hub) OnlyVia(kind string) []string {
	var out []string
	for _, a := range h.Rooms() {
		if a.Via != kind {
			continue
		}
		other := false
		for _, p := range a.Paths {
			if p.Kind != kind && (p.State == PathUp || p.State == PathOn) {
				other = true
			}
		}
		if !other {
			out = append(out, a.Name)
		}
	}
	sort.Strings(out)
	return out
}

// IndexEverywhere replaces one room's cards in the everywhere index. Called
// with what the room announced, and once per room from the cache when the hub
// starts. Reports whether the index changed.
func (h *Hub) IndexEverywhere(room string, cards []CardState) bool {
	if h.Every == nil {
		return false
	}
	return h.Every.Replace(room, cards)
}

// Index is the everywhere index as the hub feeds it from a room's announcement.
type Index interface {
	Replace(room string, cards []CardState) bool
}
