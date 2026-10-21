package cli

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/deployready"
	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
	"github.com/spf13/cobra"
)

// The hub: serve the board, hold nothing, hand everything else to a room.
//
// `atrium run --no-room` is this, and `atrium run` is this plus a room for the
// machine it runs on.

// atriumFlags is everything the central process takes on its command line.
type atriumFlags struct {
	board     string
	port      string
	dir       string
	db        string
	transport string
	advertise string
	service   string
	identity  string
	files     string
	open      bool
	// views are the roots board views are served from. See link/views.go.
	views []string

	// Reaching the board from off this machine. Empty binds loopback only,
	// which is the default and the safe one. "zrok" serves the same board
	// over a zrok share; "ziti" binds it to an OpenZiti service. See
	// atrium_share.go and link.Ziti.
	boardTransport string
	boardShareMode string
	boardService   string

	// ciHookService is the ziti service the hub binds to hear CI, empty for off. See link/cihook_bind.go.
	ciHookService string

	// Binaries to offer rooms, one per platform. Empty means this hub can
	// only offer what it is running, which is no use to a room on another
	// kind of machine. See `atrium_builds.go`.
	buildDir string
}

// bind declares the flags. `prefix` goes on the four a room also has, `dir`,
// `db`, `identity` and `service`: `atrium-` for `atrium run`, which takes the
// room's flags as well.
func (f *atriumFlags) bind(c *cobra.Command, prefix string) {
	c.Flags().StringVar(&f.board, "addr", defaultBoardAddr(),
		"where the browser reaches the board (loopback only, no login)")
	c.Flags().StringVar(&f.port, "link", defaultLinkAddr(),
		"where rooms dial in (may bind wide, e.g. 0.0.0.0:7779)")
	c.Flags().StringVar(&f.advertise, "link-advertise", "",
		"the host:port a room dials this hub at, minted into join strings (required when --link binds wide). "+
			"a host name works and each room resolves it at every dial. a comma list names more than one, the first goes in join strings")
	c.Flags().StringVar(&f.dir, prefix+"dir", "", "where this hub keeps its certificates")
	c.Flags().StringVar(&f.db, prefix+"db", "",
		"the hub's own store: which rooms exist and what they last said (default: under --"+prefix+"dir)")
	c.Flags().StringVar(&f.transport, "transport", "direct",
		"how rooms reach this hub: direct, ziti or zrok, or a comma list such as direct,ziti to listen on each. the first mints join strings")
	c.Flags().StringVar(&f.service, prefix+"service", "atrium-hub", "the ziti service, with --transport ziti")
	c.Flags().StringVar(&f.identity, prefix+"identity", "", "the ziti identity file, with --transport ziti")
	c.Flags().StringVar(&f.files, "board", "",
		"serve the board from this directory instead of the built-in copy")
	c.Flags().StringSliceVar(&f.views, "board-views", nil,
		"serve a board view at <name>.localhost for each live card whose worktree is under this directory and holds a board (repeatable)")
	c.Flags().BoolVar(&f.open, "open", false, "print the address and nothing else")
	c.Flags().StringVar(&f.buildDir, "builds", "",
		"a directory of atrium_<os>_<arch> binaries to offer rooms that asked for upgrades")
	c.Flags().StringVar(&f.boardTransport, "board-transport", "",
		"also serve the board off this machine over an overlay: none (default), zrok or ziti")
	c.Flags().StringVar(&f.boardService, "board-service", "atrium",
		"with --board-transport ziti: the ziti service to bind the board to (uses --"+prefix+"identity)")
	c.Flags().StringVar(&f.boardShareMode, "board-share", "private",
		"with --board-transport zrok: private (needs zrok on the other end) or public (a URL, no login)")
	c.Flags().StringVar(&f.ciHookService, "ci-hook-service", "",
		"bind this ziti service to hear CI webhooks, with --"+prefix+"identity. empty is off. keeps retrying while the controller is down")
}

const hubLong = "Serves the board on a port your browser opens, and listens on a second port\n" +
	"for rooms to dial in to.\n\n" +
	"THE HUB HOLDS NO WORK. No sessions, no terminals, no agent processes, and\n" +
	"no authority over any of them. Everything the board shows about a connected\n" +
	"room comes from that room, live. That is what makes it safe to restart while\n" +
	"somebody's agent is mid-sentence.\n\n" +
	"It does keep a small store of its own: which rooms exist, what they are\n" +
	"called, their join strings, and a cache of what each one last said so an\n" +
	"offline room still shows what was there. None of that is work, and none of\n" +
	"it is authoritative while a room is connected.\n\n" +
	"On first run it makes itself a certificate authority. `atrium rooms add <name>`\n" +
	"prints a join string for a room, and `atrium room join` on the machine your\n" +
	"agents are on takes it."

// atriumUp is called once the atrium is listening on both ports, before it
// starts serving the board. `atrium run` uses it to make sure this machine has
// a room. The store is open and stays open for as long as the atrium runs.
type atriumUp func(store *hubstore.Store, keys link.Keys, side *hubSide) error

// serveAtrium runs the central process until it is stopped.
func serveAtrium(f atriumFlags, up atriumUp) error {
	board, port, dir, db, transport := f.board, f.port, f.dir, f.db, f.transport
	advertise, service, files := f.advertise, f.service, f.files
	boardTransport, boardShareMode, boardService, buildDir :=
		f.boardTransport, f.boardShareMode, f.boardService, f.buildDir
	zitiIdentity = f.identity
	// THE BOARD STAYS ON LOOPBACK, checked before anything else opens, so
	// a wide --addr is refused rather than half-started. A wide room link
	// is the operator's choice and is not touched here.
	board, err := loopbackBoard(board)
	if err != nil {
		return err
	}
	raisePriority()
	keys := link.Keys{Dir: orDefault(dir, hubDir())}

	// THE HUB'S OWN STORE, and opening it is the first thing that can
	// refuse to start.
	//
	// It holds no work: no sessions, no terminals, no agent processes.
	// It holds which rooms exist, what they are called, their join
	// secrets, which are on their way out, and a cache of what each one
	// last said. Nobody else can answer any of that, because the
	// question is whether a room exists at all.
	//
	// A failure here is tier one. A hub that keeps serving while it
	// cannot remember which rooms exist will mint a second room under a
	// name it has forgotten. That the work is safe on the rooms is not
	// a reason to stay up: it is the reason the halt costs little.
	store, err := hubstore.Open(orDefault(db, filepath.Join(keys.Dir, "hub.db")))
	if err != nil {
		return fmt.Errorf("the hub's store: %w", err)
	}
	defer store.Close()
	if store.Fresh() {
		// SAID LOUDLY, because a hub pointed at the wrong directory
		// looks exactly like every room having vanished, and the rooms
		// themselves would still be dialling in and being refused.
		log.Printf("[hub] made a new store at %s. no rooms are on it yet",
			orDefault(db, filepath.Join(keys.Dir, "hub.db")))
	}
	// A hub from before this build may hold a row for its own machine as
	// a room. That feature is gone, so the row is defunct: drop it.
	if n, err := store.DropDefunctLocalRooms(); err != nil {
		return fmt.Errorf("tidying old rooms: %w", err)
	} else if n > 0 {
		log.Printf("[hub] dropped %d room(s) from when the hub could be its own room", n)
	}

	// The proxy is built after the two closures below that change what the
	// `rooms` event carries (a join string spent, a room's cards cleared),
	// so they reach it through this and skip the nudge until it exists.
	var proxy *link.Proxy
	roomsChanged := func() {
		if proxy != nil {
			proxy.RoomsChanged()
		}
	}

	// SPENDING A SECRET IS WHAT SAYS WHICH ROOM THIS IS, and the store
	// is the only thing that can answer it. Handed to the transport
	// rather than reached for, so `internal/link` never learns the hub
	// has a database.
	//
	// EVERY TRANSPORT `--transport` NAMES, AT ONCE, so a room that cannot reach
	// this hub on one comes in on the next. The first is the primary: join
	// strings are minted for it. See docs/rnd/room-transport-order.md.
	set, err := openHubs(transport, keys, port, advertise, service,
		func(secret string) (string, error) {
			r, err := store.Spend(secret)
			if err != nil {
				return "", err
			}
			roomsChanged()
			return r.Name, nil
		})
	if err != nil {
		return err
	}
	defer set.release()

	// THE HUB'S ZITI TRANSPORTS, each a switch in the gear. A flag seeds one. See link/hubtransports.go.
	var h *link.Hub
	ht := &link.HubTransports{
		Store:            store,
		IdentityFallback: f.identity,
		CheckIdentity:    func(p string) error { _, err := os.Stat(p); return err },
		Audit:            func(kind, detail string) { proxy.RecordAudit("", kind, detail) },
	}
	var zitiSide *hubSide
	for _, s := range set.sides {
		if s.kind == "ziti" {
			zitiSide = s
		}
	}
	var zrokSide *hubSide
	for _, s := range set.sides {
		if s.kind == "zrok" {
			zrokSide = s
		}
	}
	spendRoom := func(secret string) (string, error) {
		r, err := store.Spend(secret)
		if err != nil {
			return "", err
		}
		roomsChanged()
		return r.Name, nil
	}
	if zrokSide == nil {
		if s, err := newZrokHub(keys, spendRoom, nil); err == nil {
			zrokSide = s
			set.sides = append(set.sides, zrokSide)
		} else {
			log.Printf("[hub] the room link cannot be offered over zrok: %v", err)
		}
	}
	if zitiSide == nil {
		d, err := overlayDirect(keys, nil)
		if err == nil {
			z := &link.Ziti{Identity: zitiIdentity, Service: service}
			zitiSide = &hubSide{
				kind: "ziti", path: link.Path{Transport: "ziti", Service: service},
				enrol: d.ServeEnrolment, auth: link.OverlayAuthenticated,
				release: z.Close, says: "the ziti service " + service,
			}
			set.sides = append(set.sides, zitiSide)
		} else {
			log.Printf("[hub] the room link cannot be offered over ziti: %v", err)
		}
	}
	// A LATER TRANSPORT THAT CANNOT LISTEN IS LEFT OUT, NOT FATAL. The first is
	// what join strings are minted for and still refuses to start without. A
	// ziti controller that is down when the hub restarts must not keep the hub
	// down for the rooms that reach it directly. A side left out is not told to
	// rooms either, since nothing answers on it.
	var lns []net.Listener
	var lnVia []string
	var sideStates []link.ListenerState
	for i, s := range set.sides {
		if s.kind == "ziti" && i > 0 {
			continue
		}
		ln, err := s.listen()
		if err != nil {
			if i == 0 {
				return fmt.Errorf("listening for rooms over %s: %w", s.kind, err)
			}
			sideStates = append(sideStates, link.ListenerState{Kind: s.kind, LastError: err.Error()})
			log.Printf("[hub] not listening for rooms over %s, the hub runs without it: %v", s.kind, err)
			s.enabled = func() bool { return false }
			continue
		}
		defer ln.Close()
		log.Printf("[hub] listening for rooms over %s: %s", s.kind, s.says)
		sideStates = append(sideStates, link.ListenerState{Kind: s.kind, Up: true})
		lns = append(lns, ln)
		lnVia = append(lnVia, s.kind)
	}
	if zitiSide != nil && zitiSide != set.sides[0] {
		z := zitiSide
		ht.Add(link.TransportItem{
			Name: link.TransportRoomZiti, Label: "Room link over ziti", Kind: "ziti", DefaultService: service,
			Listen: roomZitiListen(keys, func(secret string) (string, error) {
				r, err := store.Spend(secret)
				if err != nil {
					return "", err
				}
				roomsChanged()
				return r.Name, nil
			}),
			Serve: func(ctx context.Context, ln net.Listener) error { return h.ServeVia(ctx, ln, "ziti") },
			Refuse: func() string {
				if rooms := h.OnlyVia("ziti"); len(rooms) > 0 {
					return "refused: " + strings.Join(rooms, ", ") + " reaches this hub only over ziti"
				}
				return ""
			},
		}, roomZitiSeed(transport, service, f.identity))
		z.enabled = func() bool { on, _ := ht.Effective(link.TransportRoomZiti); return on }
		z.service = func() string { _, svc := ht.Effective(link.TransportRoomZiti); return svc }
	}
	if zrokSide != nil && zrokSide != set.sides[0] {
		z := zrokSide
		ht.Add(link.TransportItem{
			Name: link.TransportRoomZrok, Label: "Room link over zrok", Kind: "zrok", NoIdentity: true,
			Listen: func(_, _ string) (net.Listener, error) { return z.listen() },
			Serve:  func(ctx context.Context, ln net.Listener) error { return h.ServeVia(ctx, ln, "zrok") },
			Refuse: func() string {
				if rooms := h.OnlyVia("zrok"); len(rooms) > 0 {
					return "refused: " + strings.Join(rooms, ", ") + " reaches this hub only over zrok"
				}
				return ""
			},
		}, roomZrokSeed(transport))
		z.enabled = func() bool { on, _ := ht.Effective(link.TransportRoomZrok); return on }
	}
	side := set.primary()
	side.says = set.says()

	// WHAT A HALT DOES. The store refuses every call from then on, and the
	// board stays up to say what broke.
	//
	// THE ROOM LISTENER STAYS OPEN. It used to close, on the room's
	// posture, but the hub is not the room: it holds no work, and every
	// session's control tools reach the hub through its room's link. Closing
	// it cut every session off from atrium at once (f-001). The rooms the
	// hub knew keep attaching and keep being routed to, from memory, and
	// nothing the halted store would have to write is attempted. See
	// `hubRooms`.
	store.OnHalt = func(cause error) {
		log.Printf("[hub] THE STORE HAS HALTED: %v", cause)
		log.Printf("[hub] nothing more is written. rooms this hub already knew stay attached and may reattach, " +
			"a room it did not know is refused. the agents on every room are unaffected and keep running")
	}

	h = link.NewHub(link.Timings{})
	ht.Hub = h
	for _, s := range sideStates {
		var why error
		if s.LastError != "" {
			why = errors.New(s.LastError)
		}
		h.SetListener(s.Kind, s.Up, why)
	}
	// WHAT THIS HUB CAN HAND OUT, so a room that asked to be told can
	// be told. Hashed once here rather than per attach: these are tens
	// of megabytes each and they cannot change under a running hub.
	//
	// One per platform, because a room is on another machine and a hub
	// that can only offer its own binary is no use to a fleet that is
	// not all the same. See `builds.go`.
	builds := hubBuilds(buildDir, Version)
	h.Offers(builds...)
	if what := saysWhatItHas(builds); what != "" {
		log.Printf("[hub] builds on offer, for rooms that asked: %s", what)
	}
	// THE HUB'S REPOSITORIES, mirrored from the operator's checkouts into bare copies under
	// the hub's directory and served to rooms on the `git` link kind and nowhere else. Set
	// here so the welcome says Git. See internal/gitsync and docs/fabric/git-sync-design.md.
	gitHub := &gitsync.Hub{Dir: keys.Dir, Repos: store.GitRepos, Rooms: h.GitRooms(), SelfHost: hostname()}
	// The hub's own store (git.store), read on every use, so a change needs no restart.
	gitHub.StoreRoot = func() string {
		p, _ := store.GitStorePath(keys.Dir)
		return p
	}
	h.Git = gitHub.Backend()
	// THE HUB'S OWN STORE, taken from rooms on the same kind (as that room) and from the operator on the board.
	// The push log is the hub database's git_push table (migration 0008). gitHub.Cards is set when the proxy
	// exists. A push is refused when there is no log, so the log is set before the handler is.
	gitHub.PushLog = store
	// THE MERGER, read from the hub's merge-flow roles on every push to claude/main. See docs/design/merge-flow.md.
	gitHub.Merger = func(context.Context) (string, string, bool) {
		hs, err := store.Roles(hubstore.RoleMerger)
		if err != nil || len(hs) != 1 {
			return "", "", false
		}
		return hs[0].Room, hs[0].Card, true
	}
	gitHub.CreateOnPush = func() bool {
		on, _ := store.GitCreateOnPush()
		return on
	}
	gitHub.StashRetentionDays = func() int {
		n, _ := store.StashRetentionDays()
		return n
	}
	h.GitStore = gitHub.StoreHandler()
	// A room's atrium_git_url, asked over the same kind.
	h.GitLookup = gitHub.Lookup
	// A push the hub died in the middle of is settled from the refs before anything is served: the log says
	// pending, and git says whether the ref moved.
	if err := gitHub.Reconcile(context.Background()); err != nil {
		log.Printf("[hub] could not settle the pushes the last run left pending: %v", err)
	}
	if _, err := store.GitRepos(); err != nil {
		// SAID AT STARTUP, and the hub then mirrors nothing rather than mirror a branch it
		// should not.
		log.Printf("[hub] git_repos is refused, so this hub mirrors nothing: %v", err)
	}
	h.Enrol = side.enrol
	// Every room proves who it is with a certificate this hub signed.
	h.Authenticated = func(c net.Conn) bool { return set.auth()(c) }
	// WHAT ROOMS ARE TOLD, in the order the store holds, read at every attach so
	// `atrium rooms transports` needs no restart.
	h.Paths = func() []link.Path { return set.paths(hubOrder(store.RoomTransportOrder)) }
	// A ROOM THIS HUB HAS NO RECORD OF DOES NOT ATTACH, even holding a
	// certificate this hub signed, because a certificate is not a
	// record: a room that was forced out still has its papers. The
	// same call writes down the observed half of what it says about
	// itself, which is the only moment the hub hears it.
	// WHAT THIS TRANSPORT ACTUALLY PROVES, said out loud at startup.
	//
	// Direct mTLS spends a secret bound to one room and hands back a
	// certificate carrying that name, so a room is what the hub signed.
	// An overlay proves somebody may connect and says nothing about
	// which room they are, so the name is asserted by the room and
	// checked only against the list. Nobody should have to read
	// `certs.go` to find that out.
	//
	// Over an overlay a room with a certificate is named by it, like a direct
	// one. A room that joined before that still attaches, on the overlay's
	// word alone, for as long as `overlay_legacy` allows it. The warning
	// stays while that is true, and goes when the operator refuses it.
	h.LegacyRefused = func() bool {
		refused, err := store.OverlayLegacyRefused()
		// A STORE THAT CANNOT ANSWER ALLOWS. A read error must not be the thing
		// that turns every overlay room away.
		return err == nil && refused
	}
	if set.overlay() && !h.LegacyRefused() {
		log.Printf("[hub] rooms over %s that joined with a certificate are named by it. "+
			"a room on an older join string still attaches without one, and its name is "+
			"taken on trust and checked against the list. `%s` says who, and refuses them "+
			"once everyone has re-joined", transport, atriumCmd("rooms legacy"))
	}
	h.Attaching = newHubRooms(store).attaching
	// THE NOTIFY TRIGGER. Built here so the announcement hook below can call
	// it, started and mounted once the context and the proxy exist.
	notifier := link.NewNotifier(notifyStore{store})
	// THE PERSISTENT GROWLERS, derived from the same announcements. See growl.go.
	growler := link.NewGrowler(growlStore{notifyStore{store}})
	// WHAT A ROOM SAYS IT IS HOLDING, TAKEN WHOLE. Anything the hub
	// was keeping for that room and is not in this is discarded,
	// because it is no longer there, and the discard is written down
	// rather than being silent. See `Announce` in internal/hubstore.
	h.Cached = func(name string, cards []link.CardState) error {
		r, err := store.ByName(name)
		if errors.Is(err, hubstore.ErrHalted) {
			// Attaching already let it in from memory. See below.
			return nil
		}
		if err != nil {
			return err
		}
		out := make([]hubstore.Card, 0, len(cards))
		for _, c := range cards {
			out = append(out, hubstore.Card{
				ID: c.ID, Status: c.Status, Payload: c.Payload,
			})
		}
		_, err = store.Announce(r.ID, out)
		if errors.Is(err, hubstore.ErrHalted) {
			// The cache is never authoritative. A halted store keeps the room
			// attached on the index alone rather than turn it away.
			return nil
		}
		if err == nil {
			roomsChanged()
			// AFTER THE CACHE IS WRITTEN AND NEVER WAITING ON THE SINK: the
			// notifier stores identities and hands the changes to its own queue.
			notifier.Announced(name, cards)
			growler.Announced(name, cards)
		}
		return err
	}
	// THE EVERYWHERE INDEX, built from the cache, since the rooms announce again
	// only when something changes. A room announcing replaces its own part.
	if rooms, err := store.Rooms(); err == nil {
		for _, r := range rooms {
			held, err := store.Cards(r.ID)
			if err != nil {
				continue
			}
			cards := make([]link.CardState, 0, len(held))
			for _, c := range held {
				cards = append(cards, link.CardState{ID: c.ID, Status: c.Status, Payload: c.Payload})
			}
			h.IndexEverywhere(r.Name, cards)
		}
	}

	// The board this hub serves. From disk when told to, so the loop is
	// edit, save, reload, with no rebuild at all.
	assets, id, err := boardFrom(files)
	if err != nil {
		return err
	}
	proxy = link.NewProxy(h, assets, id, nil)
	defer proxy.WaitWork(15 * time.Second)
	// ONE BOARD VIEW PER WORKER WORKTREE, at <name>.localhost. See link/views.go.
	if len(f.views) > 0 {
		for _, bad := range proxy.SetBoardViews(f.views, api.BoardID) {
			log.Printf("[hub] --board-views %s is not a directory, so no views are served from it", bad)
		}
		log.Printf("[hub] serving board views for worktrees under %s", strings.Join(f.views, ", "))
	}
	// THE DURABLE LIST, which is a different question from what is
	// attached and gets a different endpoint for exactly that reason.
	proxy.SetInventory(inventory{store: store, hub: h})
	// Input-lag logging as the gear last left it. The variable still wins.
	proxy.ApplyInputLag()
	// THE OPERATIONAL AUDIT LOG, over the hub's own store. Wired before
	// SetControl and the attach/detach callbacks below, all of which
	// record through the proxy so the line is persisted AND nudges any
	// watching board. See docs/fabric/audit-design.md.
	proxy.SetAuditLog(auditLog{store: store})
	// THE CONTROL MCP SERVER, mounted at /_hub/mcp so sessions open a
	// connection instead of each spawning an atrium-control child. It
	// reaches this hub's own board over loopback derived from `board`.
	proxy.SetResourcesDir(daemon.StateDir())
	proxy.SetControl(board)
	proxy.SetGit(gitHub)
	proxy.SetGitSettings(store, keys.Dir)
	gitHub.Audit = proxy.RecordAudit
	// A branch's owner is asked about through the hub's own relay when another card pushes to it.
	gitHub.Cards = proxy.GitCards
	// THE LAUNCH CAP PER ROOM, kept in the hub's settings. See launchcaps.go.
	proxy.SetLaunchCaps(store)
	// THE HUB'S DOCUMENTS, rows in the store and bytes beside it. See link/docs_api.go.
	proxy.SetDocs(store)
	// CHANGE REQUESTS BETWEEN ROOMS, rows in the store. See link/changerequest.go.
	proxy.SetChangeRequests(store)
	// THE BACKLOG AND DIRECTOR REPORTS EVERY ROOM SHARES, rows in the store. See link/backlog.go.
	proxy.SetBacklog(store)
	// ITEMS, WHICH THE HUB OWNS. See link/items.go.
	proxy.SetItems(store)
	// THE REPOSITORY THE MERGE FLOW READS an item's stage from: the hub setting flow.repo, as `github.com/o/r`. Unset,
	// the stage is what was reported. See link/flow_git.go.
	proxy.SetFlowGit(flowGit{store: store, hub: gitHub})
	// WHICH ROOM RUNS A PR, AND ONE ROW PER PR. See link/prclaim.go.
	proxy.SetPRClaims(store)
	// EACH ROOM'S DESIRED SPEC AND OBSERVED LOCK, served to the room itself. See link/roomspec.go.
	proxy.SetRoomSpecs(store)
	// THE HUB RUNS THE FORGE FOR ITS ROOMS, which never run gh or bb. See link/forgeroute.go.
	proxy.SetForge(store, nil)
	proxy.SetCIDir(filepath.Join(keys.Dir, "ci-artifacts"))
	// A forge branch a card asks for is fetched into the store with the same login a PR head fetch uses.
	gitHub.ForgeHelper = proxy.ForgeHelper
	// THE NAMES THE HUB ANSWERS, beside $ATRIUM_HOSTS, from the same store. Before
	// any listener is up. See hosts.go.
	link.LoadExtraHosts(store)
	// ATTACH AND DETACH AS OPERATIONAL LINES, recorded once each. Set
	// after the proxy exists because they record through it. Best effort:
	// the store's Log is fail-open and never blocks the link.
	h.OnAttach = func(name, host, ver string) {
		proxy.RecordAudit(name, "room-attached", host+" running "+ver)
		// Off the attach path, because it is a request to the room that
		// just arrived and the attach should not wait on it.
		go proxy.PushInputLag(name)
		go proxy.PushContextLimits(name)
		go proxy.PushWorkerPolicy(name)
	}
	// A ROOM ON THE OLD PATH, said once per attach. This is how the operator
	// finds who must re-join before flipping the switch: the audit line names
	// the overlay and the step, and the rooms list marks the room `proven:
	// false` for as long as it is attached.
	h.OnUnproven = func(name, over string) {
		unproven := 0
		for _, a := range h.Rooms() {
			if !a.Proven {
				unproven++
			}
		}
		log.Printf("[hub] room %q attached over %s without a certificate, so its name is "+
			"unproven. %d attached room(s) are unproven", name, over, unproven)
		proxy.RecordAudit(name, "room-unproven", "attached over "+over+
			" without a certificate. re-join it with a new join string from `"+
			atriumCmd("rooms token")+" "+name+"`")
	}
	// A PROVEN NAME CLAIMED ON THE OLD PATH, which is either somebody else or a
	// room dialling with a join string older than its certificate. The same
	// `room-unproven` kind, so `rooms log` shows it beside the attaches. See f-026.
	h.OnUnprovenRefused = func(name, over, why string) {
		proxy.RecordAudit(name, "room-unproven", "refused over "+over+" without a certificate: "+why+
			". if this is that room, check it dials with the certificate `"+atriumCmd("room join")+"` left it")
	}
	h.Enrolled = func(name string) bool {
		enrolled, err := store.Enrolled(name)
		// A STORE THAT CANNOT ANSWER REFUSES the old path for this one name, unlike
		// `LegacyRefused`, which turns everyone away. A store that errors halts, and
		// the listener closes behind it in any case.
		return err != nil || enrolled
	}
	h.Proved = func(name string) {
		if r, err := store.ByName(name); err == nil {
			_ = store.MarkEnrolled(r.ID)
		}
	}
	h.OnDetach = func(name, why string) {
		proxy.RecordAudit(name, "room-detached", why)
	}
	// THE HUB STARTED, which is also how a restart reads. Recorded once
	// here, so the board shows the gap and the next line as a restart.
	proxy.RecordAudit("", "hub-started", "the hub came up on build "+id)

	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	notifier.Start(ctx)
	proxy.SetNotify(notifier)
	proxy.SetPush(link.NewPush(store, notifier))
	proxy.SetGrowler(growler)
	growler.Start(ctx)
	startCIHook(ctx, f.ciHookService, store, ht, proxy)
	ht.Add(link.TransportItem{
		Name: link.TransportBoardZiti, Label: "Board over ziti", Kind: "board-ziti", DefaultService: "atrium-board",
		Listen: func(svc, identity string) (net.Listener, error) {
			return (&link.Ziti{Identity: identity, Service: svc}).ListenService(svc)
		},
		Serve: func(ctx context.Context, ln net.Listener) error {
			_, svc := ht.Effective(link.TransportBoardZiti)
			srv := &http.Server{
				Handler:           zitiBoardEdge(edge.MarkReach(proxy, edge.ReachOverlay), svc),
				ReadHeaderTimeout: 10 * time.Second,
			}
			go func() {
				<-ctx.Done()
				_ = srv.Close()
			}()
			return srv.Serve(ln)
		},
	}, boardZitiSeed(boardTransport, boardService))
	proxy.SetTransports(ht)
	ht.Start(ctx)

	// A room attaching is synced and collected, off the attach path. The timers stop and the
	// git children are cancelled within ten seconds of the hub stopping.
	attachedBefore := h.OnAttach
	h.OnAttach = func(name, host, ver string) {
		attachedBefore(name, host, ver)
		gitHub.Attached(ctx, name)
	}
	// WHAT GIT AND THE BUILDS SAY of an item, read again whenever a mirror pass moved, a collect ran or a room attached,
	// and on a timer for the rest. See link/lifecycle_git.go.
	gitHub.Store().Checker = func(g gitsync.CheckerGit, dir, branch string) gitsync.EvidenceChecker {
		return evidenceChecker{&deployready.Checker{Git: g, Dir: dir, Branch: branch}}
	}
	proxy.SetLandGit(landGit{store: store, hub: gitHub})
	proxy.SetHubBuild(runningCommit())
	proxy.SetHubCode(api.CodeID)
	gitHub.Observed = func() { go proxy.RefreshAllGitSteps(ctx, true) }
	{
		before := h.OnAttach
		h.OnAttach = func(name, host, ver string) {
			before(name, host, ver)
			go proxy.RefreshAllGitSteps(ctx, false)
		}
	}
	go proxy.RunGitSteps(ctx)
	go proxy.RunEfficacy(ctx)
	go proxy.RunTasks(ctx)
	// WHAT AN ITEM'S CARDS SPENT, read from each room's usage export. See link/workusage.go.
	go proxy.RunWorkUsage(ctx)
	gitHub.Start(ctx, 10*time.Second)

	// WORK THAT WAITS ON OTHER WORK, checked against the integration branch and this
	// hub's own build commit. See internal/link/deps.go.
	proxy.SetDeps(store, Commit)
	// WHETHER THE INSTALLED BUILD CAN BE REPLACED, and the one click that does it. Nothing deploys on its own.
	proxy.SetDeployReady()
	// WHAT THIS HUB RUNS, for the deploy queue to compare with the branch and with each room's own commit.
	proxy.SetRunningCommit(runningCommit())
	go proxy.RunDeps(ctx)
	// EACH ROOM'S SETUP, read on attach and every few minutes, for the rooms pill. See internal/link/setupwatch.go.
	go proxy.RunSetupWatch(ctx)

	for i, ln := range lns {
		go func(ln net.Listener, via string) {
			if err := h.ServeVia(ctx, ln, via); err != nil && ctx.Err() == nil {
				log.Printf("[hub] the room listener over %s stopped: %v", via, err)
				h.SetListener(via, false, err)
			}
		}(ln, lnVia[i])
	}

	// SNAPSHOTS OF ITS OWN STORE, because a halt on a corrupt database
	// is only tolerable if there is something to go back to. Without
	// this, "it halts" means "it is gone".
	//
	// Tiered rather than counted: everything from the last hour, one an
	// hour for a day, one a day for a week, one a week for a month. The
	// two questions people ask are "put it back to twenty minutes ago"
	// and "what did this look like last week", and a flat list of the
	// most recent fifty answers only the first.
	go store.BackUp(ctx, backupsIn(keys.Dir, db))

	// THE ADDRESS A DIRECT ROOM DIALS, checked against this machine's own. See advertisecheck.go.
	for _, s := range set.sides {
		if s.kind == "direct" {
			go watchAdvertise(ctx, s.path.Addr)
		}
	}

	// THE ADDRESSES ROOMS ARE TOLD, recomputed on a timer. A change is pushed to every attached room, so one
	// standing on another rung learns a hub that moved. See advertiseset.go.
	go watchPaths(ctx, pathsEvery, func() []link.Path { return set.paths(hubOrder(store.RoomTransportOrder)) }, h.PushPaths)

	// HANDING FREED PAGES BACK TO DISK, a bounded batch at a time while
	// the hub stays live. The room cache is rewritten wholesale on every
	// announce, so its file grows and never shrinks on its own. A no-op
	// on an older store not in incremental mode. See internal/hubstore.
	go store.VacuumLoop(ctx)
	go store.FileSweepLoop(ctx)

	srv := &http.Server{
		Addr: board,
		// The browser edge. See internal/edge.
		Handler: edge.For(board, proxy),
		// NO WRITE TIMEOUT. The event stream and the terminal are both
		// meant to stay open for hours, and a write deadline would cut
		// them with nothing to show for it.
		ReadHeaderTimeout: 10 * time.Second,
	}
	boardLn, err := net.Listen("tcp", board)
	if err != nil {
		return fmt.Errorf("board listener: %w", err)
	}

	// REACHING THE BOARD FROM ELSEWHERE, when asked. Loopback above
	// stays exactly as it was: this adds a SECOND listener over an overlay
	// and serves the SAME handler on it, so nothing is proxied and the
	// local board is untouched. The listener is released when the hub
	// stops.
	//
	// NON-FATAL, ON PURPOSE. The board share is additive: the loopback
	// board is the hub's real job and must come up even when the overlay
	// is slow, down, or unauthorised. Every failure here logs the reason
	// and the hub serves loopback anyway, rather than a transient outage
	// taking the board with it.
	if bt := strings.TrimSpace(boardTransport); bt != "" && bt != "none" {
		switch bt {
		case "zrok":
			// The public share's login, from the hub's own store: the
			// operator set it on the settings screen and it is applied
			// when the share is created. A private share ignores it. See
			// hubshare.go and docs/fabric/ziti-zrok-flow-design.md.
			shareAuth, err := store.ShareAuth()
			if err != nil {
				log.Printf("[hub] could not read the share login, "+
					"serving loopback only: %v", err)
				break
			}
			bs, shareLn, err := openBoardShare(boardShareMode, shareAuth)
			if err != nil {
				log.Printf("[hub] could not put the board on a zrok share, "+
					"serving loopback only: %v", err)
				break
			}
			defer bs.release()
			if bs.Mode == "public" {
				// A public share now always carries a login (updb or
				// OIDC); openBoardShare refuses one with none. Name the
				// scheme so the log says what guards the URL.
				log.Printf("[hub] the board is on a PUBLIC zrok share, guarded by %s at the "+
					"zrok edge", shareLoginName(shareAuth))
			}
			log.Printf("[hub] serving the board on a %s zrok share: %s", bs.Mode, bs.Address)
			proxy.RecordAudit("", "board-share-opened",
				"the board is on a "+bs.Mode+" zrok share: "+bs.Address)
			// The share's address is the one name it answers besides loopback.
			// MARKED, so the git store can refuse a public share and take a private one as the operator's.
			reach := edge.ReachZrokPrivate
			if bs.Mode == "public" {
				reach = edge.ReachZrokPublic
			}
			serveBoardOn(ctx, shareLn, edge.Named(edge.MarkReach(proxy, reach), bs.Address), "zrok share")

		case "ziti":
			// bound by the board-ziti transport, which this flag seeds
		default:
			return fmt.Errorf("no board transport called %q. one of: zrok, ziti", bt)
		}
	}

	greet(store, side, board, id, up != nil)
	if up != nil {
		// Off the serving path: making a room enrols over the link, and the
		// board should answer while that happens.
		go func() {
			if err := up(store, keys, side); err != nil {
				log.Printf("[hub] this machine's room: %v", err)
			}
		}()
	}

	go func() {
		<-ctx.Done()
		log.Printf("[hub] stopping. rooms will reconnect when it comes back")
		_ = srv.Close()
	}()
	if err := srv.Serve(boardLn); err != nil && ctx.Err() == nil {
		return err
	}
	return nil
}

// serveBoardOn serves the hub board on a second listener, an overlay's, so the
// board is reachable off this machine without touching the loopback one.
//
// What is the same for every board transport lives here: an http.Server with the
// same read-header timeout as the loopback board, closed when the hub stops. How
// the listener is obtained differs per overlay and stays at the call site, which
// is the one thing that is not shared. `what` is only for the log line.
func serveBoardOn(ctx context.Context, ln net.Listener, h http.Handler, what string) {
	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && ctx.Err() == nil {
			log.Printf("[hub] the board's %s stopped: %v", what, err)
		}
	}()
}

// greet is the first thing anybody sees, and it is the whole setup experience.
//
// WRITTEN AS THE NEXT THING TO DO, not as a status dump. Somebody running this
// for the first time has one question, "now what", and the answer is one line
// they can type.
//
// IT NO LONGER PRINTS A JOIN STRING, and that is the visible half of the hub
// naming its rooms. There is nobody to mint one for until a room has been added
// and given a name, and a string that would enrol anything under any name is
// exactly what went away. An empty hub says what it is and how to give it a
// room, which is a board that explains itself rather than one that quietly
// became something else.
func greet(store *hubstore.Store, side *hubSide, boardAddr, id string, withRoom bool) {
	fmt.Println()
	fmt.Println("  the board is at   http://localhost" + portOf(boardAddr))
	fmt.Println("  rooms dial in on  " + side.says)
	fmt.Println("  board build       " + id)
	fmt.Println()

	rooms, err := store.Rooms()
	if err != nil {
		log.Printf("[hub] could not read the room list: %v", err)
		return
	}
	if len(rooms) == 0 && withRoom {
		fmt.Println("  No rooms yet. This machine's room is being made now, and shows on the")
		fmt.Println("  board when it attaches.")
		fmt.Println()
		return
	}
	if len(rooms) == 0 {
		fmt.Println("  No rooms yet, and a hub with no room has nothing to show. Give it one:")
		fmt.Println()
		fmt.Println("      " + atriumCmd("rooms add") + " <name>")
		fmt.Println()
		fmt.Println("  That prints a join string for that name and nothing else. Paste it into")
		fmt.Println("  `" + atriumCmd("room join") + "` on the machine your agents are on.")
		fmt.Println()
		return
	}
	fmt.Printf("  %d room(s) on this hub:\n", len(rooms))
	for _, r := range rooms {
		fmt.Println("      " + r.Name + "   " + sinceHeard(r))
	}
	fmt.Println()
	fmt.Println("  `" + atriumCmd("rooms ls") + "` says more. `" + atriumCmd("rooms add") +
		" <name>` adds one.")
	fmt.Println()
}

// sinceHeard is a room's line on that list, in the terms somebody reads it in.
func sinceHeard(r hubstore.Room) string {
	switch {
	case r.Marked():
		return "marked for deletion"
	case r.LastSeen == nil:
		// The state the durable list exists for. A room that has never
		// connected is inventory, not work.
		return "never connected"
	case r.LikelyAttached():
		return "attached"
	default:
		return "last heard from " + r.LastSeen.Local().Format("2006-01-02 15:04")
	}
}

// boardFrom picks the board this hub serves and hashes it.
//
// The hash matters more than it looks: the board compares it against what
// `/v1/health` reports and reloads itself when they differ. The hub answers
// that field on the room's behalf, because the hub is the thing serving the
// files. See `rewriteHealth` in internal/link.
func boardFrom(dir string) (fs.FS, string, error) {
	if strings.TrimSpace(dir) == "" {
		return api.EmbeddedBoard(), api.BuildID, nil
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, "", err
	}
	if _, err := os.Stat(filepath.Join(abs, "index.html")); err != nil {
		return nil, "", fmt.Errorf("%s does not look like a board: no index.html", abs)
	}
	fsys := os.DirFS(abs)
	log.Printf("[hub] serving the board from %s", abs)
	return fsys, api.BoardID(fsys), nil
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

// advertiseFor decides what a join string carries for the room link.
//
// A wide bind is not dialable, so a token minted from it would carry an address
// no room can reach. The rules:
//
//   - An explicit --link-advertise always wins, whatever the bind is.
//   - A loopback bind (127.0.0.1, ::1) keeps 127.0.0.1, which is the default and
//     the two-accounts-on-one-box case.
//   - An explicit wide bind (0.0.0.0, ::) with no advertise is REFUSED rather
//     than coerced to loopback. Coercing it mints a loopback token that looks
//     right and fails for every remote room, which is the silent failure this
//     exists to stop. The operator hands the reachable address on
//     --link-advertise.
//   - An empty host (":PORT") keeps today's loopback default: it is the shape
//     the default flag ships with and the historical two-accounts case relies
//     on it.
func advertiseFor(bind, override string) (string, error) {
	// A comma list is a set of addresses. The join string carries the first.
	if o, _, _ := strings.Cut(override, ","); strings.TrimSpace(o) != "" {
		return strings.TrimSpace(o), nil
	}
	host, port, err := net.SplitHostPort(bind)
	if err != nil {
		return "", fmt.Errorf("--link %q is not a host:port: %w", bind, err)
	}
	if host == "0.0.0.0" || host == "::" {
		return "", fmt.Errorf(
			"--link is bound wide to %s but --link-advertise is not set. rooms cannot "+
				"dial %s, so the join string would be useless. pass --link-advertise "+
				"<host:port> with the address a room reaches this hub at", bind, host)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	return net.JoinHostPort(host, port), nil
}

// loopbackBoard checks the board's bind address and pins it to loopback.
//
// THE BOARD HAS NO LOGIN. That is the invariant in the root CLAUDE.md and in
// docs/fabric/overlays.md: loopback and no login stays true, and reaching the board
// from elsewhere is an overlay's job, not a wide bind on a port with no auth.
// This makes the invariant enforced rather than assumed.
//
// An empty host (`:7800`) binds every interface, so it is coerced to 127.0.0.1
// rather than refused: the default must stay usable and must also stay safe. An
// explicit non-loopback host is the operator asking for the one thing this
// refuses, so it is refused with the reason and the alternative.
func loopbackBoard(addr string) (string, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return "", fmt.Errorf("--addr %q is not a host:port for the board: %w", addr, err)
	}
	if host == "" {
		host = "127.0.0.1"
	}
	if !isLoopbackHost(host) {
		return "", fmt.Errorf(
			"--addr %q would put the board on a non-loopback address, and the board "+
				"has no login, so anything that can reach it can read every command and "+
				"answer permission requests. keep --addr on loopback (127.0.0.1). to reach "+
				"the board from elsewhere use an overlay (--board-transport zrok); to let a "+
				"room dial in from elsewhere bind --link wide instead", addr)
	}
	return net.JoinHostPort(host, port), nil
}

// isLoopbackHost is true for an address that names only this machine.
//
// A loopback IP or the name localhost. A different hostname is refused rather
// than resolved: a name that happens to point at loopback today can point
// elsewhere tomorrow, and the board's safety must not depend on what a resolver
// says at start.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func portOf(addr string) string {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return ":" + port
}

// zitiBoardEdge is the hub board's browser edge on a ziti service. See zitiEdge
// in internal/daemon: the intercept address is the network's, so $ATRIUM_HOSTS
// names it, and with none the service answers any Host, said once.
func zitiBoardEdge(h http.Handler, service string) http.Handler {
	if len(edge.EnvNames()) == 0 {
		log.Printf("[hub] the ziti service %q answers any Host. set %s to its intercept address "+
			"to refuse a rebound one", service, edge.EnvHosts)
		return edge.Unnamed(h)
	}
	return edge.Named(h)
}

// flowGit is the merge flow's reach into the hub's repository, named by the `flow.repo` hub setting on every use.
type flowGit struct {
	store *hubstore.Store
	hub   *gitsync.Hub
}

func (f flowGit) repo() string { return landGit{store: f.store}.repo() }

func (f flowGit) ItemFacts(ctx context.Context, branch, sha string) (gitsync.ItemFacts, error) {
	if name := f.repo(); name != "" {
		return f.hub.Store().ItemFacts(ctx, name, branch, sha)
	}
	return gitsync.ItemFacts{}, nil
}

// landGit is the lifecycle's reach into the same repository, named the same way.
type landGit struct {
	store *hubstore.Store
	hub   *gitsync.Hub
}

func (f landGit) repo() string {
	name, _ := f.chooseRepo()
	return name
}

// chooseRepo is the repository the flow reads: the flow_repo setting, else the one repository the hub holds. With none
// to choose it answers why, in words a step can carry.
func (f landGit) chooseRepo() (name, why string) {
	set, _ := f.store.HubSetting(hubstore.SettingFlowRepo)
	repos, _ := f.store.GitRepos()
	var held []string
	for _, r := range repos {
		held = append(held, r.Name)
	}
	return pickFlowRepo(set, held)
}

// pickFlowRepo is the choice itself, pure. A named repository wins, a hub holding exactly one stands in for an unset
// name, and anything else is no repository and the reason.
func pickFlowRepo(set string, held []string) (name, why string) {
	if set = strings.TrimSpace(set); set != "" {
		return set, ""
	}
	switch len(held) {
	case 0:
		return "", "flow_repo is unset and the hub holds no repository"
	case 1:
		return held[0], ""
	}
	return "", "flow_repo is unset and the hub holds " + strconv.Itoa(len(held)) + " repositories, so none can be chosen"
}

func (f landGit) LandFacts(ctx context.Context, branch, sha, workID string, forge func() gitsync.ForgeMain) (gitsync.LandFacts, error) {
	if name := f.repo(); name != "" {
		return f.hub.Store().LandFacts(ctx, name, branch, sha, workID, forge)
	}
	_, why := f.chooseRepo()
	return gitsync.LandFacts{Why: why}, nil
}

// evidenceChecker is deployready.Checker in gitsync's words.
type evidenceChecker struct{ c *deployready.Checker }

func (e evidenceChecker) Since(ctx context.Context, from, to string) (int, error) {
	return e.c.Since(ctx, from, to)
}

func (e evidenceChecker) Coverage(ctx context.Context, base, tip string, alsoOn ...string) (gitsync.Coverage, error) {
	cov, err := e.c.Coverage(ctx, base, tip, alsoOn...)
	return gitsync.Coverage{Total: cov.Total, Covered: cov.Covered, Held: cov.Held, TipCovered: cov.TipCovered}, err
}

// ItemEvidence is the tested and reviewed steps' reach into the same repository. See link.EvidenceGit.
func (f landGit) ItemEvidence(ctx context.Context, branch string, shas []string) (gitsync.Evidence, error) {
	if name := f.repo(); name != "" {
		return f.hub.Store().ItemEvidence(ctx, name, branch, shas)
	}
	return gitsync.Evidence{}, nil
}

// RepoName and RevertScan are the efficacy collectors' reach into the same repository.
func (f landGit) RepoName() string { return f.repo() }

func (f landGit) RevertScan(ctx context.Context) ([]gitsync.Revert, error) {
	if name := f.repo(); name != "" {
		return f.hub.Store().RevertScan(ctx, name)
	}
	return nil, nil
}

func (f landGit) ForgeMain(ctx context.Context, force bool) gitsync.ForgeMain {
	if name := f.repo(); name != "" {
		return f.hub.Store().ForgeMainTip(ctx, name, force)
	}
	return gitsync.ForgeMain{Why: "no repository is set for the merge flow"}
}

// PageChangedSince is the page-changed hint's reach into the same repository. See link.PageChangeGit.
func (f landGit) PageChangedSince(ctx context.Context, since, path string) (gitsync.PageChange, bool, error) {
	name := f.repo()
	if name == "" {
		return gitsync.PageChange{}, false, nil
	}
	return f.hub.Store().PageChangedSince(ctx, name, since, path)
}

// FileAt is the landed check's reach into the same repository. See link.DocsFileGit.
func (f landGit) FileAt(ctx context.Context, sha, path string) (string, bool, error) {
	name := f.repo()
	if name == "" {
		return "", false, nil
	}
	return f.hub.Store().FileAt(ctx, name, sha, path)
}

func (f landGit) Contains(ctx context.Context, build, sha string) (bool, bool, error) {
	if name := f.repo(); name != "" {
		return f.hub.Store().Contains(ctx, name, build, sha)
	}
	return false, false, nil
}

func (f flowGit) CoveredBy(ctx context.Context, base, sha string) (bool, error) {
	if name := f.repo(); name != "" {
		return f.hub.Store().CoveredBy(ctx, name, base, sha)
	}
	return false, nil
}

func (f flowGit) EpochPlan(ctx context.Context) (gitsync.EpochPlan, error) {
	name := f.repo()
	if name == "" {
		return gitsync.EpochPlan{}, errors.New("no repository is set for the merge flow: set it with atrium_flow repo")
	}
	return f.hub.Store().EpochPlan(ctx, name)
}

func (f flowGit) EpochMove(ctx context.Context, n int64, branch, expectOld string) (string, string, error) {
	name := f.repo()
	if name == "" {
		return "", "", errors.New("no repository is set for the merge flow: set it with atrium_flow repo")
	}
	return f.hub.Store().EpochMove(ctx, name, n, branch, expectOld)
}
