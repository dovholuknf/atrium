// Package daemon wires the three layers together: the store owns state, the
// hub serves agents, and the api serves humans.
//
// The two listeners are separate so one can be closed without the other. When
// the store halts the agent-facing listener closes and does not reopen, so
// every runner sees connection-refused and parks on the backoff it already has,
// while the human-facing listener stays up to say what broke.
package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/mcprule"
	"github.com/dovholuknf/atrium/internal/shellpick"
	"github.com/dovholuknf/atrium/internal/store"
)

// Options configures a daemon.
type Options struct {
	AgentAddr string        // agent-facing listener, e.g. ":7777"
	HumanAddr string        // human-facing listener, e.g. ":7778"
	DBPath    string        // sqlite file
	LongPoll  time.Duration // caps how long shutdown waits on parked agent requests
	// Room is the hub-facing name this daemon is known by, when it is a room
	// attached to a hub. It is exported to every launched session as
	// ATRIUM_ROOM, so the session's HTTP control MCP registration can send
	// X-Atrium-Room and the hub can scope its control calls to this room. Empty
	// for a daemon with no hub, where there is no room to name.
	Room string
	// ShutdownToken guards POST /v1/shutdown. Empty means loopback only.
	// Setting one says the endpoint is meant to be reachable remotely.
	ShutdownToken string
	// LocationFile is where this daemon records the address it is listening
	// on. Empty means the machine's one true place for it, which is what a
	// real daemon wants and what every caller looks in.
	//
	// It exists so a test can point somewhere else. Run writes this file on
	// start and DELETES it on stop, so a test that starts a daemon and stops
	// it was removing the address of whatever real daemon was running at the
	// time. Nothing broke, because a caller that cannot find the file falls
	// back to the default port and the daemon under test was on the default
	// port anyway, which is exactly why it went unnoticed. On a non-default
	// port it would have quietly unhooked every live session instead.
	LocationFile string
	// Passive means SERVE THE BOARD AND TOUCH NOTHING ELSE.
	//
	// For `atrium preview`, which opens a COPY of a database so a change to
	// the board can be judged by using it. Everything in that copy is real:
	// real fixtures, real shares, real cards. An ordinary start acts on all of
	// them, so the first preview spawned the operator's runners and tried to
	// take their zrok name off them, from a process that was supposed to be a
	// window onto a copy.
	//
	// What it turns off is everything that reaches outside the process:
	// fixtures, restoring lent shares, and sweeping dead ones. What stays on
	// is the store, the API, the board, the event stream and the ability to
	// attach to something you started HERE, because those are what is being
	// looked at.
	//
	// Not a security boundary, and not a read-only mode. Somebody using a
	// preview board can still launch a runner or shelve a card in the copy.
	// The rule is narrower and it is about STARTUP: opening a database is not
	// consent to act on what is in it.
	Passive bool
	// BoardDir serves the board from this directory instead of the embedded
	// copy, so changing the page is a browser refresh rather than a restart
	// that every supervised terminal on the machine pays for. Empty is the
	// default and is the embed. See api.Server.BoardDir.
	BoardDir string
	// RoomDir is where a room keeps its certificate (`atrium room --dir`).
	// It is only recorded in the location file, so a restart can pass it back.
	RoomDir string
	// StartedBy is the `--started-by <kind>:<nonce>` a supervisor gave this
	// process. KEPT IN MEMORY ONLY: never exported to the environment and never
	// passed to anything this process spawns, so no child of a supervised room
	// can claim to be one. `POST /v1/preflight` reports it. Empty when nothing
	// supervised the start.
	StartedBy string
}

// Daemon owns the store and both listeners.
type Daemon struct {
	// wakeLaunch replaces the real launch when a done card is woken. Tests only. See keep.go.
	wakeLaunch func(LaunchRequest) (*store.Task, error)
	// authLim bounds password guessing on the published board. See auth_limit.go.
	authLim authLimiter
	// forgeOpen is the forge access alerts open now, by tool@host. See forgeaccess.go.
	forgeMu   sync.Mutex
	forgeOpen map[string]ForgeAlert
	// acProbe knows which runners take --autocompact. See autocompact.go.
	acProbe *autocompactProbe
	// windingDown is set when shutdown begins. A session ending after it is the
	// wind-down, never somebody deciding. See the session hook's `end`.
	windingDown atomic.Bool
	// bootResumes is resume id to the card on the reopen list that takes it,
	// for the length of one reopen pass. See reopenSaved.
	bootResumes sync.Map
	// modelWaits is card id to a /model switch the input gate has not let through
	// yet. See modelswitch.go.
	modelWaits sync.Map
	// modelLocks is card id to the mutex one switch holds. See modelLock.
	modelLocks sync.Map

	opts Options
	st   *store.Store
	ap   *api.Server
	// prr runs pull request reviews. See prrunner.go.
	prr *prRunner

	// mergedCulling is set while the sweep is culling due workers, so a slow
	// exit is not started twice by the next tick. See mergedcull.go.
	mergedCulling atomic.Bool

	// perms holds the hook connections parked on a permission answer. See
	// permwait.go.
	perms *permWait

	// sup holds the runners atrium owns, when a harness launches in pty mode.
	sup *supervisor
	// starting holds the ids of cards whose launch has not finished, so the board can draw them as starting.
	starting sync.Map

	// The pty host link. See hostterm.go.
	ph hostLink

	// humanTouched is when each card's human touch was last written, so a run
	// of keystrokes costs one map lookup each and one store write a minute. See
	// humanTouch in park.go.
	humanTouched sync.Map
	// decideWho is who is deciding a permission while the decide route resolves
	// it: permission id to decider. Held only for the call. See decideBy.
	decideWho sync.Map
	// idle is what the idle parking tick remembers between ticks: a handoff under
	// way, and the idle clock a handoff turn must not move. See idletick.go.
	idle idleParks

	// roomView is the last size a viewer agreed on for any runner, loaded from
	// the store on first use. See roomsize.go.
	roomMu     sync.Mutex
	roomView   viewport
	roomLoaded bool

	// act holds what each runner is doing right now, in memory only. See
	// docs/runtime/activity-design.md.
	act *activityTracker
	// esc is which agent-launched cards the board should hear about, and how
	// many times it has. In memory, like the activity it is derived from: a
	// restart recomputes it on the next tick. See a2a.go.
	esc escalations
	// scans is the last screen read of each quiet card. See launchstuck.go.
	scans termScans
	// looksIdleFired counts every looks-idle firing since start. See looksidle.go.
	looksIdleFired atomic.Int64

	// settle is how long this daemon still calls an arriving card part of its
	// own restart rather than news. See settling.go.
	settle settling

	// up holds the runner version lookups in flight, so a wave of launches
	// produces one request. See runnerupdate.go.
	up updates

	// nats holds any overlay listener the board is being served on, so it can
	// be reached from somewhere else. See overlay_native.go.
	nats   map[overlayKind]*native
	natsMu sync.Mutex

	// guests holds sessions lent out one at a time, each on its own share that
	// reaches that session and nothing else.
	//
	// In memory, and that is now only half the story: the ADDRESS is recorded
	// in the store and comes back after a restart. What is here is the live
	// listener, which cannot outlive the process. See overlay_guest.go, and
	// `docs/fabric/overlays.md` for why an address that dies with the daemon was the
	// wrong answer.
	guests guestShares

	// rooms holds the other machines reporting into this hub. In memory and
	// nowhere else: a room's cards are that room's state, and a second durable
	// copy here would be a source of truth that is wrong whenever the room is
	// unreachable. See rooms.go.
	rooms rooms

	// peerLimit bounds how often one session may message others. In memory,
	// because the thing it bounds is a runaway session and a session does not
	// outlive the daemon either.
	peerLimit *peerLimiter

	// relays is the way to other rooms, through the hub, and the drain of what
	// is owed to them. Empty on a daemon that is not a room. See relay.go.
	relays relayState

	// pending retries the on-screen delivery of peer messages the gate would not
	// take right now, on a widening backoff. In memory, because the durable copy
	// is the queued message row and the hooks deliver that whatever this does.
	// See pendinginject.go.
	pending *pendingInjector

	// wake holds the after-restart wakes, mirrored from the store, and when each
	// card's session last started. See restartwake.go.
	wake *wakes

	// holds is the room's deploy hold, copied from the store for the permission
	// path, and build is the binary it was set against. See roomhold.go.
	holds *holdState
	build string

	// nctx holds the new-context sequences under way and the failed ones nobody
	// has dismissed. In memory. See newcontext.go.
	nctx *newContexts

	// ka keeps idle Claude cards' prompt caches warm. See keepalive.go.
	ka *keepalive

	// ctx is each live Claude card's context size, read from its transcript.
	// See contextsize.go.
	ctx *contextSizes
	// output is when each card's transcript last gained a reply. See outputat.go.
	output outputTimes

	// announced is every conversation id a SessionStart announced for a card
	// (key "card|id"), and resumeNoted the refusals already written onto a
	// card's history, so a Stop every turn does not repeat itself. See
	// resumeclaim.go.
	announced   sync.Map
	resumeNoted sync.Map

	// usage records every Claude card's token use, a row per turn. See usage.go.
	usage *usageTracker
	// opencode reads opencode cards' sessions, made on first use. See opencodereader.go.
	opencode     *opencodeReader
	opencodeOnce sync.Once

	// limitLast is the last limit figure kept per card and kind, so a repeated
	// statusline post writes nothing. See keepLimitReadings.
	limitMu   sync.Mutex
	limitLast map[string]string

	// ledgerDirty asks the snapshot writer to rewrite work-ledger.md. One slot,
	// so any number of changes while a write is under way are one more write.
	// See ledger.go.
	ledgerDirty chan struct{}

	// unseen holds the cards whose latest turn nobody has seen, so an operator
	// keystroke can answer "does this see a turn" from memory. The durable copy
	// is the turn_seen table. See seen.go.
	unseen sync.Map

	// stop is how a shutdown request reaches the wind-down Run is waiting on.
	stop *stopper

	// launching serializes the check-then-spawn region of a launch, keyed by the
	// card and the resume id. Without it two restarts or launches onto one card
	// both pass the "is a runner live" guard before either registers, and both
	// resume the same conversation. See keyedmutex.go and launch.go.
	launching *keyedMutex
	// startedAt is when a launch last put a runner onto a card, keyed by card id,
	// so a repeat of that launch answers with the card rather than a refusal. See
	// repeatLaunch in launch.go.
	startedAt sync.Map

	// closeOnce guards releasing the store, so the shutdown path and a caller's
	// deferred Close cannot both close the database. See closeDB.
	closeOnce sync.Once
	// clearOnce guards removing the address file, which the shutdown path does
	// before it closes the store and Run's deferred call would do again after.
	// See clearLocation.
	clearOnce sync.Once

	// gone is what the reaper remembers about worktrees that have vanished. See
	// worktreegone.go.
	gone goneWatch

	// hubGit is the room's stable hub remote. See hubremote.go.
	hubGit hubGit
	// hubForge is the hub's forge, for a room with a hub. See hubforge.go.
	hubForge hubForge

	mu          sync.Mutex
	agentServer *http.Server
}

// StateDir returns the directory atrium keeps its own state in: a hub folder
// under WORKTREE_ROOT when that is set, otherwise ~/.atrium.
//
// No hardcoded drive path. One machine's layout is not a default for another,
// and a home directory exists everywhere.
//
// The `hub` segment stays. It is where every existing database with
// WORKTREE_ROOT set already lives, and a daemon that opens a new empty
// database shows a board that has lost every card.
func StateDir() string {
	if root := strings.TrimRight(os.Getenv("WORKTREE_ROOT"), `\/`); root != "" {
		return filepath.Join(root, "hub")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".atrium")
	}
	return ".atrium"
}

// DefaultDBPath puts the database next to the rest of atrium's state.
func DefaultDBPath() string {
	dir := StateDir()
	if dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			dir = filepath.Join(home, ".atrium")
		} else {
			dir = "."
		}
	}
	return filepath.ToSlash(filepath.Join(dir, "atrium.db"))
}

// New opens the store and builds the daemon. A storage failure here is fatal
// by design: the daemon refuses to start rather than run without durable state.
func New(opts Options) (*Daemon, error) {
	if opts.LongPoll == 0 {
		opts.LongPoll = time.Minute
	}
	if opts.DBPath == "" {
		opts.DBPath = DefaultDBPath()
	}
	if dir := filepath.Dir(opts.DBPath); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("create %s: %w", dir, err)
		}
		// MkdirAll leaves an existing directory as it was, so a room made
		// before this was 0700 is tightened here. The directory holds the
		// session key, the OIDC secret and every card's launch_env.
		if err := os.Chmod(dir, 0o700); err != nil {
			log.Printf("[atrium] chmod %s: %v", dir, err)
		}
	}
	st, err := store.Open(opts.DBPath)
	if err != nil {
		return nil, err
	}
	// Review run folders live beside the database unless the reviews_root setting
	// says otherwise.
	st.DefaultReviewsRoot = filepath.Join(filepath.Dir(opts.DBPath), "reviews")
	d := &Daemon{
		opts: opts, st: st, perms: newPermWait(), ap: api.New(st),
		sup: newSupervisor(), act: newActivityTracker(), stop: newStopper(),
		nats:      map[overlayKind]*native{},
		peerLimit: newPeerLimiter(),
		launching: newKeyedMutex(),
	}
	d.hubGit.cards = &gitsync.CardTokens{Live: d.cardLive}
	d.pending = newPendingInjector(d)
	d.wake = newWakes()
	d.holds = newHoldState()
	d.build = buildIdentity()
	d.nctx = newNewContexts()
	d.nctx.persist = d.saveNewContextJournal
	// Input-lag logging as the gear last left it, so a room that restarts keeps
	// timing if it was timing. The variable still wins. See internal/inputlag.
	api.ApplyInputLag(st)
	// Card icons live beside the database, which is the one directory atrium
	// already owns and already backs up with the rest of its state.
	api.IconDir = filepath.Join(filepath.Dir(opts.DBPath), "icons")
	// Pasted files go here when they are not being kept, and the directory is
	// emptied on the way up rather than on the way down. A daemon that was
	// killed never runs its own cleanup, and the guarantee somebody wants from
	// this setting is that the pictures are gone, not that they were deleted
	// tidily.
	api.ScrapDir = filepath.Join(filepath.Dir(opts.DBPath), "scrap")
	// A card's own state, its overlays first. Closing the card deletes its part (api/cardoverlay.go).
	api.CardsDir = filepath.Join(filepath.Dir(opts.DBPath), "cards")
	// Where the lean plugin directories go, in atrium's own state and not a worktree.
	leanPluginRoot = filepath.Join(filepath.Dir(opts.DBPath), "lean-plugins")
	if err := os.RemoveAll(api.ScrapDir); err != nil {
		log.Printf("[atrium] could not empty %s: %v", api.ScrapDir, err)
	}
	st.OnHalt = d.onHalt
	api.HeldNoticesOf = d.heldNoticesFor
	api.OwedOf = d.owedFor
	d.ledgerDirty = make(chan struct{}, 1)
	st.OnLedgerChange = d.ledgerChanged
	st.OnLedgerNotice = d.ledgerNotice
	st.RemoteArbiter = d.launcherRelay
	st.HoldNotice = func(arbiter *store.Task, source string) bool {
		if source == NoticeReport {
			return holdsReports(arbiter)
		}
		if source == NoticeFYI {
			return holdsNotices(arbiter)
		}
		return holdsNotices(arbiter)
	}
	st.OnRelayHeld = d.kickRelays
	d.ap.BoardDir = opts.BoardDir
	d.ap.Decide = d.decide
	d.ap.DecideBy = d.decideBy
	d.ap.Room = opts.Room
	d.ap.Launch = d.launchFromJSON
	d.ap.Kill = d.Kill
	d.ap.RunSource = d.RunSourceNow
	d.ap.Recognise = d.Recognise
	d.prr = newPRRunner(st, d.ap.PublishPR)
	d.ap.PRRunner = d.prr
	d.prr.onAccess, d.prr.onWorked = d.ForgeAccessFrom, d.ForgeWorked
	d.prr.hub, d.prr.hubSource = d.HubForge, d.hubSource
	d.ap.PRForge = d.prr.forgeFor
	d.ap.HubSource = func(ctx context.Context, name string) (string, func(), error) {
		if d.HubForge() == nil {
			return "", nil, errNoHub
		}
		return d.hubSource(ctx, name)
	}
	d.ap.ForgeFailed, d.ap.ForgeWorked = d.ForgeAccessFrom, d.ForgeWorked
	d.ap.SCMClone = d.prWorktreeClone
	d.ap.Stash = d.StashTo
	d.ap.RunAction = d.handleRunAction
	d.ap.CancelPending = d.CancelPending
	d.ap.Settling = d.Settling
	d.ap.DrainAuto = d.drainForAuto
	d.ap.Attach = d.handleAttach
	d.ap.OpenShell = d.handleShellOpen
	d.ap.ShutShell = d.handleShellClose
	d.ap.OlderScrollback = d.handleOlderScrollback
	d.ap.RawScrollback = d.handleRawScrollback
	d.ap.TextScrollback = d.handleTextScrollback
	d.ap.TypingState = d.handleTypingState
	d.ap.DismissAsks = d.handleDismissAsks
	d.ap.Message = d.handleMessage
	d.ap.Say = d.handleSay
	d.ap.Move = d.moveHandler()
	d.ap.TaskSays = d.handleTaskSays
	d.ap.RoomPeers = d.handleRoomPeers
	d.ap.RoomCard = d.handleRoomCard
	d.ap.RoomExit = d.handleRoomExit
	d.ap.RoomLaunch = d.handleRoomLaunch
	d.ap.Report = d.handleReport
	d.ap.GitClone = d.handleGitClone
	d.ap.HubRemote = d.HubRemoteBase
	d.ap.HubGitURL = d.handleHubGitURL
	d.ap.RestartWake = d.handleRestartWake
	d.ap.Hold = d.handleHold
	d.ap.NewContext = d.handleNewContext
	d.ap.Resume = d.handleResume
	d.ap.SendNote = d.handleSendNote
	d.ap.SwitchModel = d.handleModel
	d.ap.Shutdown = d.handleShutdown
	d.ap.Preflight = d.handlePreflight
	d.ap.ForgeAccess = d.ForgeAlerts
	d.ap.Shelve = d.Shelve
	d.ap.StopRunner = d.StopRunner
	d.ap.StopRunnerBy = d.StopRunnerBy
	d.ap.Cull = func(id, into, tip string) (any, error) { return d.CullProved(id, into, tip) }
	d.ap.HoldCull = d.HoldCull
	d.ap.GitPush = d.GitPush
	d.ap.Merged = func(into string, branches []string) (any, error) { return d.Merged(into, branches) }
	d.ap.MergeProof = func(dir, ref, into string) (any, error) { return d.MergeProof(dir, ref, into) }
	d.ap.ArchiveWorkers = func(dryRun bool) (any, error) { return d.ArchiveWorkers(dryRun) }
	d.ap.RestartRunner = d.RestartRunner
	d.ap.Unshelve = d.Unshelve
	d.ap.Overlays = d.overlayViews
	d.ap.SaveOverlay = d.saveOverlay
	d.ap.StartOverlay = d.startOverlay
	d.ap.StopOverlay = d.stopOverlay
	d.ap.SetupOverlay = func(kind string, body []byte) (string, error) {
		var req SetupRequest
		if err := json.Unmarshal(body, &req); err != nil {
			return "", err
		}
		return d.setupOverlay(kind, req)
	}
	d.ap.TeardownOverlay = d.teardownOverlay
	d.ap.InspectToken = d.InspectToken
	d.ap.ReserveName = d.ReserveZrokName
	d.ap.Capabilities = func() any { return d.ZitiCapabilities() }
	d.ap.ZrokAccount = func() any { return d.ZrokAccount() }
	d.ap.SetApiEndpoint = d.SetZrokEnvironment
	d.ap.ShareCard = d.ShareCard
	d.ap.StopCardShare = d.StopCardShare
	d.ap.GuestShares = d.GuestShares
	// The secret is NEVER sent back, only whether there is one. This board is
	// itself something people open and screenshot.
	d.ap.AuthConfig = func() any {
		c := d.authConfig()
		return map[string]any{
			"enabled": c.Enabled, "issuer": c.Issuer, "client_id": c.ClientID,
			"redirect": c.Redirect, "allow": c.Allow,
			"has_client_secret": strings.TrimSpace(c.ClientSecret) != "",
			// The name is shown, the password never is: only whether one is
			// set, which is the difference between drawing "set a password"
			// and "change it". The hash and its salt do not leave the daemon
			// either, since a hash somebody has is a hash somebody can attack
			// offline.
			"basic": c.Basic, "user": c.User, "has_password": c.HasPassword(),
		}
	}
	d.ap.SaveAuth = func(body []byte) error {
		// The stored secret is carried over when the form sends none, so
		// saving any other field does not silently blank it. A form that has
		// never been shown the secret cannot send it back.
		next := d.authConfig()
		var in struct {
			Enabled      *bool    `json:"enabled"`
			Issuer       *string  `json:"issuer"`
			ClientID     *string  `json:"client_id"`
			ClientSecret *string  `json:"client_secret"`
			Redirect     *string  `json:"redirect"`
			Allow        []string `json:"allow"`
			Basic        *bool    `json:"basic"`
			User         *string  `json:"user"`
			// The password ARRIVES IN PLAIN and is never stored that way. It
			// is hashed below and this field is the only place it exists in
			// this process. `GET /v1/auth` answers whether one is set and
			// never what it is, so the form cannot send back what it was
			// shown, which is what the empty-means-leave-it rule below is for.
			Password *string `json:"password"`
		}
		if err := json.Unmarshal(body, &in); err != nil {
			return err
		}
		if in.Enabled != nil {
			next.Enabled = *in.Enabled
		}
		if in.Issuer != nil {
			next.Issuer = *in.Issuer
		}
		if in.ClientID != nil {
			next.ClientID = *in.ClientID
		}
		if in.Redirect != nil {
			next.Redirect = *in.Redirect
		}
		if in.Allow != nil {
			next.Allow = in.Allow
		}
		// Empty means "leave it alone". Clearing one means sending a single
		// space, which is odd and is the safer way round: the alternative
		// blanks a working secret every time somebody edits the allow list.
		if in.ClientSecret != nil && strings.TrimSpace(*in.ClientSecret) != "" {
			next.ClientSecret = strings.TrimSpace(*in.ClientSecret)
		}
		if in.Basic != nil {
			next.Basic = *in.Basic
		}
		if in.User != nil {
			next.User = strings.TrimSpace(*in.User)
		}
		// Same rule as the client secret, and the same reason: a form that was
		// never shown the password cannot send it back, so an empty one means
		// leave it rather than clear it. Otherwise editing the name would blank
		// the password every time.
		if in.Password != nil && strings.TrimSpace(*in.Password) != "" {
			if err := setBasicPassword(&next, *in.Password); err != nil {
				return err
			}
		}
		return d.SaveAuth(next)
	}
	d.ap.Rooms = d.Rooms
	d.ap.RoomCheckIn = d.handleRoomCheckIn
	d.ap.RoomDecide = d.handleRoomDecide
	d.ap.RoomJoin = d.RoomJoin
	d.ap.RoomForget = d.handleRoomForget
	d.ap.Dispatches = d.Dispatches
	d.ap.QueueDispatch = d.QueueDispatch
	d.ap.CancelDispatch = d.CancelDispatch
	d.ap.DispatchResult = d.handleDispatchResult
	d.ap.BuildExport = func() (any, error) { return d.BuildExport() }
	d.ap.ApplyImport = func(body []byte, apply, force bool) (any, error) {
		var in Export
		if err := json.Unmarshal(body, &in); err != nil {
			return nil, fmt.Errorf("that is not an atrium configuration: %w", err)
		}
		return d.ApplyImport(&in, apply, force)
	}
	// The board only offers attach for a runner atrium owns, because a window
	// mode launch has no terminal here to show.
	api.IsSupervised = func(taskID string) bool { return d.sup.get(taskID) != nil }
	api.IsStarting = func(taskID string) bool { _, ok := d.starting.Load(taskID); return ok }
	// Whether this card has a shell open, so the board can offer `open a shell`
	// or `go to the shell` rather than guessing. Asked rather than remembered
	// in the page, because a shell outlives a reload and a board that tracked
	// it itself would be wrong after every restart.
	api.HasShell = func(taskID string) bool { return d.sup.getShell(taskID) != nil }
	api.CloseShellFor = d.CloseShell
	api.CheckLeanGateway = func(name string) error { return checkLeanGateway(d.st, name, os.ReadFile) }
	// How many rings the scrollback setting is being multiplied by right now,
	// so the settings box can say what the number it holds costs in total.
	api.LiveRings = d.sup.ringCount
	// What a runner is doing right now. Held in the daemon, never written down.
	api.ActivityOf = d.activityFor
	// How much context it has burned. Held the same way and for the same
	// reason. See docs/runtime/statusline-telemetry.md.
	api.TelemetryOf = d.telemetryFor
	// Which agent-launched cards are stuck, for the board to ring about. Held
	// the same way. See a2a.go.
	api.EscalationOf = d.escalationFor
	// The after-restart wake waiting on a card.
	d.loadWakes()
	api.RestartWakeOf = d.wakeFor
	// The deploy hold, and which cards it holds. See roomhold.go.
	d.loadHolds()
	api.HeldOf = d.heldFor
	api.NewContextOf = d.newContextFor
	// The cache keep-alive: each card's switch and its current idle stretch,
	// and the per-card switch the board flips. See keepalive.go.
	d.ka = newKeepalive(st)
	d.ka.broadcast = d.ap.Broadcast
	d.ka.window = func(taskID string) int {
		if t := d.act.telemetry(taskID); t != nil {
			return t.Window
		}
		return 0
	}
	api.KeepaliveOf = d.ka.view
	d.ap.SetKeepalive = d.keepaliveSet
	// Each card's context size, held the same way. See contextsize.go.
	d.ctx = newContextSizes()
	d.ka.holding = d.nctx.holding
	d.ka.deployHeld = d.deployHeld
	d.ka.session = d.ctx.sessionOf
	d.acProbe = newAutocompactProbe()
	api.ContextSizeOf = d.contextSizeFor
	api.AutocompactOf = d.autocompactFor
	api.OutputAtOf = d.outputAtFor
	// Token use on record, read only by a card's details. See usage.go.
	d.usage = newUsageTracker(st)
	d.usage.broadcast = d.ap.Broadcast
	// A keep-alive refresh's row is announced the same way as a turn's.
	d.ka.spent = func(u *store.SessionUsage) error {
		d.usage.stamp(u)
		err := st.AddSessionUsage(u)
		if err == nil {
			d.usage.emitRow(u)
		}
		return err
	}
	d.ap.UsageOf = d.usageFor
	d.ap.Replies = func(id string, n int, before time.Time) (any, error) {
		return d.repliesPage(id, n, before)
	}
	d.ap.Changes = d.changesFor
	// Starting a fixture is spawning a process, which the daemon owns.
	api.StartFixture = d.StartFixtureNow
	// Which turns are unread, carried across the restart. See seen.go.
	d.loadUnseen()
	return d, nil
}

func (d *Daemon) launchFromJSON(body []byte) (*store.Task, error) {
	var req LaunchRequest
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	// Somebody is looking at a dialog waiting for this, which is the one case
	// where the version check is allowed to hold a launch up. Set here rather
	// than read off the body so nothing on the wire can claim it.
	req.Interactive = true
	req.SpawnedBy = launcherFor(req)
	if err := d.launchHeld(req); err != nil {
		return nil, err
	}
	return d.Launch(req)
}

// decide resolves a permission from a human client. It goes through the parked
// request so the blocked runner is actually released: recording the decision
// without signalling the reply channel the hook is waiting on would leave that
// runner hanging.
func (d *Daemon) decide(permID, decision, reason, command string) (*store.Permission, error) {
	return d.decideBy(permID, decision, reason, command, "")
}

// DecidedByHubAuto is the one decider a caller of the decide route may name:
// the hub's board-wide auto switch (r-020). Everything else a caller sends is
// refused, so no caller can forge a rule's name or another decider.
const DecidedByHubAuto = "global-auto"

// decideBy is decide with the decider named. "" is the operator by hand, as
// always. `by` is stashed for the permission, and whichever path records the
// decision (the parked agent's, or the fallback below) reads it back, so both
// record the same decider.
func (d *Daemon) decideBy(permID, decision, reason, command, by string) (*store.Permission, error) {
	if by != "" {
		d.decideWho.Store(permID, by)
		defer d.decideWho.Delete(permID)
	}
	p, err := d.decideInner(permID, decision, reason, command)
	// A person answering is the human touch. A rule or auto mode answering is
	// not, and never comes through here with DecidedBySelf.
	if err == nil && p != nil && p.DecidedBy == store.DecidedBySelf {
		d.humanTouch(p.TaskID, ViaPermission)
	}
	return p, err
}

func (d *Daemon) decideInner(permID, decision, reason, command string) (*store.Permission, error) {
	if command != "" {
		// Record the rewrite before releasing the agent, so the audit log shows
		// what actually ran rather than what was asked for.
		if err := d.st.RewriteCommand(permID, command); err != nil {
			return nil, err
		}
	}
	if d.decideByStoreID(permID, decision, reason, command) {
		// That called onPermDecided, which recorded it.
		return d.st.GetPermission(permID)
	}
	// Nothing is blocked on it: the agent gave up, or the daemon restarted
	// while the request was pending. Record the decision anyway so the queue
	// does not keep showing it.
	p, err := d.st.DecidePermissionBy(permID, decision, reason, d.decidedBy(permID))
	if err != nil {
		return nil, err
	}
	d.publishTask(p.TaskID)
	d.ap.Broadcast("permission", p)
	return p, nil
}

// decidedBy is who is deciding this permission right now: the decider stashed by
// decideBy, else the operator by hand.
func (d *Daemon) decidedBy(permID string) string {
	if v, ok := d.decideWho.Load(permID); ok {
		if by, _ := v.(string); by != "" {
			return by
		}
	}
	return store.DecidedBySelf
}

// Store exposes the store.
func (d *Daemon) Store() *store.Store { return d.st }

// Close releases the database.
func (d *Daemon) Close() error {
	if d.prr != nil {
		d.prr.Stop()
	}
	d.stopOutput()
	return d.closeDB()
}

// closeDB releases the store at most once.
//
// The shutdown path closes it explicitly so the detached room restarter's
// invariant is real rather than incidental (see shutdown and
// cmd/atrium2/restart.go), and a caller that keeps a `defer d.Close()` must not
// then close it a second time. The once makes both callers safe.
func (d *Daemon) closeDB() error {
	var err error
	// The link first, so a daemon torn down leaves the host and its runners up.
	d.ph.close()
	d.closeOnce.Do(func() { err = d.st.Close() })
	return err
}

// runnerFound is api.RunnerFound, a variable so tests can skip a PATH walk
// that costs a stat per entry per runner on every daemon they start.
var runnerFound = api.RunnerFound

// reportRunners says which configured runners this machine actually has.
//
// Printed at startup because PATH is read when the process starts. Installing
// something afterwards is invisible until the daemon is restarted, and a
// runner that fails to launch for that reason gives no hint that a restart is
// the answer.
func (d *Daemon) reportRunners() {
	hs, err := d.st.Harnesses()
	if err != nil || len(hs) == 0 {
		return
	}
	width := 0
	for _, h := range hs {
		if len(h.ID) > width {
			width = len(h.ID)
		}
	}
	log.Printf("[atrium] runners, resolved by explicit path or against this process's PATH:")
	missing := 0
	for _, h := range hs {
		state := "off"
		if h.Enabled {
			state = "on "
		}
		if p := runnerFound(h); p != "" {
			log.Printf("[atrium]   %-*s  %s  %s", width, h.ID, state, p)
			continue
		}
		log.Printf("[atrium]   %-*s  %s  NOT FOUND (%s)", width, h.ID, state, h.Exe())
		if h.Enabled {
			missing++
		}
	}
	if missing > 0 {
		log.Printf("[atrium] %d enabled runner(s) cannot start. install them, then restart "+
			"the daemon so it picks up the new PATH.", missing)
	}
}

// onHalt closes the agent-facing listener and leaves it closed.
func (d *Daemon) onHalt(cause error) {
	log.Printf("[atrium] HALTED: %v", cause)
	log.Printf("[atrium] agent listener closing. runners will park on connection-refused and burn nothing.")
	log.Printf("[atrium] fix the cause and restart. atrium will not recover on its own.")
	d.mu.Lock()
	srv := d.agentServer
	d.agentServer = nil
	d.mu.Unlock()
	if srv != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
	}
	d.ap.Broadcast("halted", map[string]string{"cause": fmt.Sprint(cause)})
	// And as `health`, the event the board reads instead of polling /v1/health.
	d.publishHealth()
}

// observedFor builds the observed bucket from what the wire name tells us.
// v1 agents send only a name, so that is all there is until the agent learns
// to send a registration payload.
func observedFor(agent string) store.Observed {
	host, _ := os.Hostname()
	return store.Observed{WireName: agent, Runner: "claude", Hostname: host}
}

func (d *Daemon) onPermRequest(req PermissionRequest) (string, *AutoDecision, error) {
	obs := observedFor(req.Agent)
	// The hook reports the runner's own pid and working directory. The pid is
	// what makes free liveness checks possible.
	obs.PID = req.PID
	// A nested `claude` inherits the parent's name, and its own pid must not
	// replace the parent's on the card. The request is still answered in full.
	if req.PID > 0 {
		if t, err := d.st.GetByWireName(req.Agent); err == nil && !d.ownsSession(t, req.PID) {
			log.Printf("[atrium] permission for %s from pid %d, which is not its runner "+
				"(a nested session?): gated, but its pid is not recorded", t.ID, req.PID)
			obs.PID = 0
		}
	}
	obs.NameSource = req.NameSource
	if req.Cwd != "" {
		obs.Worktree = strings.ReplaceAll(req.Cwd, `\`, "/")
	}
	task, _, err := d.st.Register(obs)
	if errors.Is(err, store.ErrStaleName) {
		// A directory name only a finished card holds. A gated call must
		// still land on a card, or the board has nowhere to ask and the hook
		// fails open, so this falls back to matching as a told name.
		log.Printf("[atrium] permission from %q: name is only held by a finished card, matching it anyway", req.Agent)
		obs.NameSource = ""
		task, _, err = d.st.Register(obs)
	}
	if err != nil {
		return "", nil, err
	}
	tool, command := req.Tool, req.Command
	// A permission request is a tool starting, so it doubles as an activity
	// report. A gated session therefore shows live activity with no activity
	// hook wired up, at no cost: this call is already being made.
	d.act.set(task.ID, ActivityTool, tool)
	// This card has a hook that carries messages in. Written once per card.
	if task.ToolHookSeenAt == nil {
		if err := d.st.SawHook(task.ID, store.HookTool); err != nil {
			return "", nil, err
		}
	}
	// A `Task` call used to be counted here as a subagent starting. It is not
	// any more, and removing it fixes two things rather than one.
	//
	// It DOUBLE COUNTED. `SubagentStart` is wired now and reports the same
	// subagent through the activity path, so a gated session with both showed
	// two for every one.
	//
	// It also LEAKED. Nothing decremented it: the count went up when the tool
	// call was requested, including when the request was then refused, and
	// only `SubagentStop` ever brings a count down. A denied Task call left a
	// subagent on the card that had never existed and would never end.
	//
	// Inferring a subagent from a tool name was the right thing to do when
	// there was no hook that said so. There is one now.
	// The key makes a retry the same question rather than a new one. A daemon
	// that crashed between recording a decision and answering would otherwise
	// ask twice, and the second answer would be given against a situation that
	// had moved on. Empty means the store treats this as distinct.
	// The runner's own id for this attempt beats a hash of the command, which
	// cannot tell a retry from the same command run tomorrow. When one is sent
	// the request is deduplicated exactly and the replay window does not
	// apply. A hook that sends only a hash keeps the window.
	key := req.DedupKey
	if id := strings.TrimSpace(req.ToolUseID); id != "" {
		key = store.ExactKey(id)
	}
	p, decided, err := d.st.RecordPermission(task.ID, tool, command, key, req.Details)
	if err != nil {
		return "", nil, err
	}
	if decided {
		// Recorded, because a replay reaches an agent as a real answer and
		// the log is what the gate is justified by. Without this a refusal
		// arrives with nothing anywhere saying it happened, which is the one
		// thing that log exists to prevent.
		if err := d.st.AppendEvent(task.ID, store.EventPermDecided, map[string]any{
			"id": p.ID, "decision": p.Decision, "reason": p.Reason,
			"by": "replay", "tool": tool, "command": command,
		}); err != nil {
			log.Printf("[atrium] could not record a replayed decision: %v", err)
		}
		return p.ID, &AutoDecision{Decision: p.Decision, Reason: p.Reason}, nil
	}

	// Anything queued for this session rides the next tool call, which is how a
	// message reaches a session that is working. The call is refused in order
	// to carry the text, and the banner says so, or the model reads a delivery
	// as a judgement on its command.
	msgs, merr := d.takeMessages(task.ID, "permission")
	if merr == nil && len(msgs) > 0 {
		reason := messageBanner(msgs, true)
		if _, err := d.st.DecidePermissionBy(p.ID, "block", reason, store.DecidedByMessage); err != nil {
			return "", nil, err
		}
		d.publishTask(task.ID)
		return p.ID, &AutoDecision{Decision: "block", Reason: reason}, nil
	}

	// A shelved card is a standing no. Putting work down has to answer for that
	// work, or the agent asks, gets nothing, and freezes behind a card nobody
	// is looking at any more.
	if task.Status == store.StatusShelved {
		if _, err := d.st.DecidePermissionBy(p.ID, "block", shelvedReason, "shelved"); err != nil {
			return "", nil, err
		}
		return p.ID, &AutoDecision{Decision: "block", Reason: shelvedReason}, nil
	}

	// A room deploy hold refuses every held card's next call, so every agent is
	// idle with nothing in flight when the room restarts. After shelved, whose own
	// no is more specific, and before rules and auto mode, which it overrides. See
	// roomhold.go.
	if h := d.deployHold(); h.Holds(task.ID) {
		d.noteHoldWorking(h, task.ID)
		reason := d.deployRefusal(h)
		if _, err := d.st.DecidePermissionBy(p.ID, "block", reason, DecidedByDeployHold); err != nil {
			return "", nil, err
		}
		return p.ID, &AutoDecision{Decision: "block", Reason: reason}, nil
	}

	// A standing rule short-circuits the human entirely. The request is still
	// recorded and resolved, so the history shows what ran and which rule let
	// it through, but nothing is ever shown to be clicked.
	rule, err := d.st.MatchRule(tool, command, task.Worktree)
	if err != nil {
		return "", nil, err
	}
	if rule != nil {
		// DecidePermissionBy writes the audit event, carrying what answered
		// this in `by`. A second one here would show every rule decision
		// twice.
		//
		// What is recorded as the decider is the rule's pattern, and an MCP rule's
		// pattern is always `*`, which names nothing. Its tool is what says which
		// rule this was.
		by := rule.Prefix
		if mcprule.Is(rule.Tool) && rule.Prefix == mcprule.AnyInput {
			by = rule.Tool
		}
		if _, err := d.st.DecidePermissionBy(p.ID, rule.Decision, rule.Reason, by); err != nil {
			return "", nil, err
		}
		return p.ID, &AutoDecision{Decision: rule.Decision, Reason: rule.Reason}, nil
	}

	// Auto mode: stop asking, keep recording.
	//
	// Last, after messages, shelving and standing rules. Auto mode stops new
	// questions; it does not discard answers already given, so a never rule and
	// a shelved card both still block. What it lets through goes to the same
	// audit log as every other decision, marked auto, and the review reads it.
	//
	// Global auto is the same thing for every session at once, including ones
	// that do not exist yet. It is recorded under its own name rather than as
	// `auto`, because "I turned this session loose" and "I turned the whole
	// board loose" are different answers to give six hours later.
	//
	// A deadline is read here rather than enforced by a timer. This is the
	// only moment auto mode means anything, so it is the only moment worth
	// asking the clock, and a check made here cannot be missed by a restart.
	//
	// A card whose deadline has passed has its flag turned off on the way
	// through, so the badge on the board stops claiming something that stopped
	// being true. Lazy on purpose: the state that matters is the answer this
	// request gets, and the write is bookkeeping that follows it.
	at := time.Now().UTC()
	if task.AutoExpired(at) {
		if err := d.st.SetAutoApprove(task.ID, false); err != nil {
			log.Printf("[atrium] clearing expired auto mode on %s: %v", task.ID, err)
		} else {
			log.Printf("[atrium] auto mode on %s ran out, so it is asking again",
				task.DisplayTitle())
			task.AutoApprove, task.AutoUntil = false, nil
			d.publishTask(task.ID)
		}
	}
	if global := d.st.GlobalAuto(); task.AutoOn(at) || global {
		by, reason := "auto", autoReason
		if global && !task.AutoOn(at) {
			by, reason = "global-auto", globalAutoReason
		}
		if _, err := d.st.DecidePermissionBy(p.ID, "approve", reason, by); err != nil {
			return "", nil, err
		}
		d.ap.Broadcast("permission", p)
		return p.ID, &AutoDecision{Decision: "approve", Reason: reason}, nil
	}

	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		return "", nil, err
	}
	d.publishTask(task.ID)
	d.ap.Broadcast("permission", p)
	return p.ID, nil, nil
}

// autoReason is recorded against every request auto mode lets through, so the
// audit log separates "a human said yes" from "nobody was asked".
const autoReason = "auto mode: approved without asking, and recorded"

// globalAutoReason names the board-wide switch rather than the per-session one,
// so an agent told why it was let through says which of the two answered.
const globalAutoReason = "global auto mode: every session is approved without asking, and recorded"

// shelvedReason is handed back to an agent whose request was refused because
// its card is shelved, so the agent is told why rather than left waiting.
const shelvedReason = "this task is shelved in atrium. unshelve it to answer requests from it."

// CancelPending answers every outstanding request on a task with a block.
//
// Moving a card out of a waiting state has to answer the question, not just
// hide it. Otherwise the agent stays frozen with nobody coming, and the request
// sits in the queue to be approved later against a situation that has moved on.
func (d *Daemon) CancelPending(taskID, reason string) (int, error) {
	pending, err := d.st.PendingForTask(taskID)
	if err != nil {
		return 0, err
	}
	for _, p := range pending {
		// Through decide, so the blocked agent is actually released rather
		// than having its answer written to a row it never reads.
		if _, err := d.decide(p.ID, "block", reason, ""); err != nil {
			return 0, err
		}
	}
	if len(pending) > 0 {
		log.Printf("[atrium] blocked %d pending request(s) on %s: %s", len(pending), taskID, reason)
	}
	return len(pending), nil
}

func (d *Daemon) onPermDecided(permID, decision, reason string) {
	p, err := d.st.DecidePermissionBy(permID, decision, reason, d.decidedBy(permID))
	if err != nil {
		log.Printf("[atrium] decide %s: %v", permID, err)
		return
	}
	// Permission resolved means the runner goes back to work.
	if err := d.st.SetStatus(p.TaskID, store.StatusRunning); err != nil {
		log.Printf("[atrium] status after decision: %v", err)
	}
	d.publishTask(p.TaskID)
	d.ap.Broadcast("permission", p)
}

func (d *Daemon) publishTask(id string) {
	t, err := d.st.Get(id)
	if err != nil {
		return
	}
	d.ap.PublishTask(t)
}

// Run serves both listeners until ctx is canceled or a listener fails.
// BoardHandler is the human-facing surface: the JSON API, the event stream, the
// terminal websocket and the board's own files.
//
// EXPORTED SO A ROOM CAN SERVE IT SOMEWHERE ELSE. `internal/link` runs this
// same handler on connections the room dialled out to a hub, which is how the
// board can be restarted without touching a single running agent. It is the
// same handler the loopback listener uses, not a copy and not a subset: a room
// whose hub is down is still a working atrium on its own address, and that is
// the escape hatch the whole split depends on.
//
// NOT the agent listener. That one is a different mux on a different port and
// `docs/fabric/overlays.md` says never to publish it. See `Run` below.
func (d *Daemon) BoardHandler() http.Handler { return d.ap.Handler() }

func (d *Daemon) Run(ctx context.Context) error {
	go d.probeAutocompact()
	d.resumePRReviews()
	agentMux := http.NewServeMux()
	agentMux.HandleFunc("/permission", d.handlePermission)
	agentMux.HandleFunc("/session", d.handleSession)
	agentMux.HandleFunc("/gate", d.handleGate)
	agentMux.HandleFunc("/stop", d.handleStop)
	agentMux.HandleFunc("/activity", d.handleActivity)
	agentMux.HandleFunc("/telemetry", d.handleTelemetry)
	// A session declaring its work over, which nothing could say before.
	agentMux.HandleFunc("/finish", d.handleFinish)
	agentMux.HandleFunc("/ready", d.handleReady)
	// The other half of finish: a session saying it is stuck and what it needs,
	// on its card for a human or routed to a named peer.
	agentMux.HandleFunc("/help", d.handleHelp)
	// And the return leg, which is the same bus carrying an answer back and
	// taking the question off the card it was on.
	agentMux.HandleFunc("/answer", d.handleAnswer)
	// Sessions addressing each other. On the AGENT listener, because that is
	// what a session can already reach, and `docs/fabric/overlays.md` says never to
	// publish this port. A peer bus is the first feature that gives anybody a
	// reason to want it reachable, and the answer is still no: two machines
	// talking is the forum's job, not this one's.
	agentMux.HandleFunc("/peers", d.handlePeers)
	agentMux.HandleFunc("/tell", d.handleTell)
	agentMux.HandleFunc("/hooks-changed", d.handleHooksChanged)
	// The room's stable hub remote: a card's git reaches the hub's store through here, on its own token, and
	// the token opens this route and no other. See hubremote.go.
	agentMux.Handle(gitsync.HubRemotePrefix, d.hubRemote())

	// THE BROWSER EDGE on both: a web page on this machine cannot write here,
	// rebind a name onto it, or open a terminal. See internal/edge.
	agentSrv := &http.Server{Addr: d.opts.AgentAddr, Handler: edge.For(d.opts.AgentAddr, agentMux)}
	humanSrv := &http.Server{Addr: d.opts.HumanAddr, Handler: edge.For(d.opts.HumanAddr, d.BoardHandler())}

	d.mu.Lock()
	d.agentServer = agentSrv
	d.mu.Unlock()

	agentLn, err := net.Listen("tcp", d.opts.AgentAddr)
	if err != nil {
		return fmt.Errorf("agent listener: %w", err)
	}
	d.setAgentAddr(agentLn.Addr())
	// NO BOARD OF ITS OWN, when asked for with `-`.
	//
	// A room attached to a hub is reached through that hub, and its loopback
	// board is a second address showing the same thing. For a room running
	// INSIDE a hub there is not even a fallback argument for it: they are one
	// process, so a hub that is down takes the loopback board with it.
	//
	// `-` rather than empty, because empty is a valid address meaning every
	// interface on a random port, which is the opposite of what somebody
	// leaving this blank would want.
	var humanLn net.Listener
	if strings.TrimSpace(d.opts.HumanAddr) != "-" {
		humanLn, err = net.Listen("tcp", d.opts.HumanAddr)
		if err != nil {
			agentLn.Close()
			return fmt.Errorf("human listener: %w", err)
		}
		// The profiler, on this listener only and only when it is loopback.
		// See pprof.go.
		humanSrv.Handler = withProfiling(humanSrv.Handler, humanLn)
	}

	// `addressOf`, not concatenation. An address that already names a host,
	// which is what `atrium preview` passes, came out as
	// `http://localhost127.0.0.1:53895`.
	log.Printf("[atrium] agents  -> %s", addressOf(d.opts.AgentAddr))
	if strings.TrimSpace(d.opts.HumanAddr) == "-" {
		log.Printf("[atrium] board   -> none of its own. reached through the hub")
	} else {
		log.Printf("[atrium] board   -> %s", addressOf(d.opts.HumanAddr))
	}
	log.Printf("[atrium] state   -> %s", d.opts.DBPath)
	// WHAT A SHELL WILL OPEN AS, said once at startup rather than discovered
	// by opening one. The search looks at PATH, so the answer is a property of
	// the machine the daemon is running on and nobody can guess it from
	// outside. Somebody whose board gave them the wrong shell needs to know
	// what was FOUND before they can say what to use instead, and the
	// `shell_command` setting is where they say it.
	log.Printf("[atrium] shell   -> %s", shellpick.Chosen())

	// Before the address file is overwritten, since the previous one is what
	// says which database the last daemon used.
	d.warnIfDifferentDatabase()

	// Written once both listeners are bound, so the file never advertises an
	// address that failed to open.
	d.writeLocation()
	defer d.clearLocation()

	// A new database is indistinguishable from every card and every rule having
	// vanished. WORKTREE_ROOT unset once sent the path to the home directory,
	// and a hundred and twenty five rules appeared to be gone.
	d.reportRunners()

	// Cards a crash interrupted mid-turn, read before the reaper or a reopen
	// moves their status. See unexpectedexit.go.
	d.noteCrashMidTurn()

	if d.st.Fresh() {
		log.Printf("[atrium] ---------------------------------------------------------------")
		log.Printf("[atrium] THIS IS A NEW DATABASE. There was no file at that path, so one")
		log.Printf("[atrium] was created. The board will be empty and you will have no rules.")
		log.Printf("[atrium] If you expected your existing state, you are pointed at the")
		log.Printf("[atrium] wrong path. Check WORKTREE_ROOT, or pass --db explicitly.")
		log.Printf("[atrium] ---------------------------------------------------------------")
	}
	// Free liveness: ask the operating system whether each runner still
	// exists, rather than asking the runner.
	go d.reap(ctx, ReapEvery)
	// The room's own stats, pushed to the board. See roomstats.go.
	d.startRoomStats(ctx)
	// Handing the space a prune or an event roll-off freed back to disk, a
	// bounded batch at a time while the room stays live. A no-op on an older
	// database not in incremental auto_vacuum mode. See vacuum.go.
	go d.vacuumLoop(ctx, VacuumEvery)
	// The commands that find work. Its own loop rather than the reap ticker,
	// because a source runs on the interval its own row names and the reaper
	// asks one question at one rate. Nothing here can halt anything: intake is
	// a suggestion, and a source that fails says so on its row.
	go d.sourceLoop(ctx)
	// EVERYTHING BELOW THIS REACHES OUTSIDE THE PROCESS, and a passive daemon
	// does none of it. See `Options.Passive`: a preview opened on a COPY of
	// somebody's database inherits their fixtures and their shares, and the
	// first thing it did was spawn their runners and try to take their zrok
	// name off them. A board being looked at must not act on what it is
	// drawing.
	if !d.opts.Passive {
		// Everything below puts cards back that were here before. Say so, so
		// the board re-seeds rather than announcing each one as news. See
		// settling.go, and note this opens BEFORE the goroutine: a window that
		// started inside it would race the first fixture.
		d.settle.begin()
		// Say so on the stream, and say again when the window closes. See health.go.
		go d.watchSettle(ctx)
		// Terminals that come up with the daemon, and then the ones that were
		// simply open when it stopped. In the background, so a runner that is
		// slow to start cannot delay the board answering: a board that is not
		// up yet looks like a hang, a terminal that is not open yet does not.
		//
		// IN THAT ORDER. A fixture card is on the reopen list too, and the
		// fixture is what pins it, themes it and decides how it resumes. See
		// `reopenSaved`.
		// A deploy hold this room restarted under is lifted before any runner
		// starts, so every wake it queues is newer than the runner that takes it.
		// See roomhold.go.
		d.liftAtStartup()
		// Before any runner is reopened: a clear the last process left half done.
		d.endAbandonedNewContexts()
		go func() {
			// Cleared at the end of this function and nowhere else.
			//
			// Without it the window closes in the gap BETWEEN the two stages:
			// `startFixtures` empties the set it named, and `reopenSaved` has
			// not named its own yet, so for an instant nothing is pending and
			// the board decides the restart is over. That instant is where the
			// arrivals it was supposed to swallow actually land.
			defer d.settle.arrived(settleBoot)
			// Terminals a pty host kept while no daemon was here, BEFORE fixtures so a card that already has
			// a live runner is not started a second time. Whatever the setting says: off is the rollback, and a
			// host still holding runners must be picked up or the next start would run a second copy of each.
			d.reattachRuns()
			d.startFixtures()
			d.reopenSaved()
			// Throwaways whose session ended when the last daemon did, so
			// nothing was there to clean up after them. Last, because it reads
			// which cards have a runner and the two calls above are what
			// decide that. See throwaway.go.
			d.sweepThrowaways()
			// What the inventory lists that no live card holds, once the cards above are back. It marks what is
			// gone and lists the rest for the board. Nothing is removed. See api/sweep.go.
			d.ap.Sweep(ctx)
		}()
		// Sessions that were lent out when the last daemon went down. A
		// restart is not the operator withdrawing a link, so the address comes
		// back up rather than the link going dead. Anything whose runner is
		// not up yet is left to `EnsureCardShare`, on the path a runner takes.
		d.RestoreCardShares()
		// And whatever is recorded against cards that have since been pruned,
		// which nothing else can see: the board draws cards, and the card is
		// what went. Only names atrium reserved and recorded, never anything
		// else on the account.
		go d.SweepDeadCardShares()
	}
	// Cards named before atrium asked git. Once, at startup, rather than on
	// registration: registration runs on every hook of every session, and a
	// directory in no repository would re-answer that question forever.
	if n, err := d.st.BackfillGitInfo(); err != nil {
		log.Printf("[atrium] could not name cards from their repositories: %v", err)
	} else if n > 0 {
		log.Printf("[atrium] named %d card(s) from their repository and branch", n)
	}
	// Resident sessions named before a name became a default alias: saorch
	// takes `@saorch`. Once, ever. See store.BackfillDefaultAliases.
	if n, err := d.st.BackfillDefaultAliases(); err != nil {
		log.Printf("[atrium] could not give cards their default aliases: %v", err)
	} else if n > 0 {
		log.Printf("[atrium] gave %d card(s) the alias their name makes", n)
	}
	// The work ledger: the one-time backfill, the items no exit path reached,
	// and the snapshot beside the database. See ledger.go.
	d.startLedger()
	go d.ledgerWriter(ctx)
	// Wakes queued before the restart, typed in as their cards come back. A
	// passive board brings nothing back, so it has nothing to type into.
	if !d.opts.Passive {
		go d.wakeLoop(ctx)
		// A deploy hold nobody redeployed under is lifted when it runs out.
		go d.holdLoop(ctx)
		// Idle caches kept warm. A passive board spends nothing on anybody's
		// behalf. See keepalive.go.
		go d.ka.loop(ctx)
	}
	log.Printf("[atrium] ready. ctrl-c to stop.")

	errCh := make(chan error, 2)
	go func() {
		err := agentSrv.Serve(agentLn)
		// A halt closes this listener, so its shutdown is not an error to exit
		// on.
		if errors.Is(err, http.ErrServerClosed) {
			return
		}
		errCh <- fmt.Errorf("agent listener: %w", err)
	}()
	if humanLn != nil {
		go func() {
			err := humanSrv.Serve(humanLn)
			if errors.Is(err, http.ErrServerClosed) {
				return
			}
			errCh <- fmt.Errorf("human listener: %w", err)
		}()
	}

	select {
	case <-ctx.Done():
		log.Printf("[atrium] interrupt received, shutting down")
	case <-d.stop.ch:
		// The same wind-down as ctrl-c, reachable from anywhere the board is.
		// Killing the process takes every supervised runner with it.
		log.Printf("[atrium] stopping: %s", d.stop.why())
	case err := <-errCh:
		log.Printf("[atrium] listener failed, shutting down: %v", err)
		d.shutdown(agentSrv, humanSrv)
		return err
	}
	d.shutdown(agentSrv, humanSrv)
	return nil
}

// shutdown closes both listeners and says what it is doing at each step.
// Agents parked in a long poll keep the connection open until the poll expires,
// so this can take several seconds. Silence for that long looks like a hang.
func (d *Daemon) shutdown(servers ...*http.Server) {
	start := time.Now()
	// Every session ends from here on, and none of those endings is somebody
	// deciding. See the session hook's `end`.
	d.windingDown.Store(true)

	// SAY IT IS COMING, BEFORE ANYTHING GOES.
	//
	// Every window has to tell two things apart: a session that ended, and a
	// daemon on its way down. They look identical from a browser, which is why
	// this is announced rather than inferred.
	//
	// Inferring it afterwards cannot be made to work. Asking `/v1/health` gives
	// the wrong answer, because the listener below is still up while every
	// runner is being stopped. Timing out and hoping is what left a popped-out
	// window sitting on a dead terminal until somebody pressed F5.
	//
	// One event, ahead of the teardown, and every window switches into "expect
	// this, and come back" for itself. After it, a socket closing means the
	// restart. Without it, a socket closing means that session ended.
	//
	// FIRST, because `d.ap.Close()` on the next line releases the streams this
	// travels on. Nothing is stopped between here and there.
	d.ap.Broadcast("going-down", map[string]any{
		"why": d.stop.why(), "at": time.Now().UTC().Format(time.RFC3339),
	})
	// A moment for it to reach the sockets. Publishing is a channel send per
	// subscriber and the write happens on their own goroutines, so without this
	// the close below can beat the message onto the wire, which is the one
	// ordering that makes the announcement pointless.
	time.Sleep(120 * time.Millisecond)

	// Release the event streams. Each open browser tab holds one, and Shutdown
	// waits for in-flight requests, so leaving them open is what made the board
	// listener sit out the whole grace period.
	d.ap.Close()

	// Any share goes with the board it publishes. An address that outlives
	// what it points at answers with a connection refused, which reads as the
	// overlay being broken.
	d.closeOverlays()

	// The deferred-injection retries stop first, so a backoff timer cannot fire
	// against a store that is closing under it. The queued messages stay on disk
	// and the next daemon's hooks deliver them. See pendinginject.go.
	if d.pending != nil {
		d.pending.stopAll()
	}
	// And any new-context sequence: its terminal is about to close under it.
	d.nctx.stopAll()

	// Which runners were mid-turn, read before they are stopped: a runner that
	// exits files its card dead or done. See unexpectedexit.go.
	d.noteStopMidTurn()

	// Runners atrium owns get a real chance to wind up before their terminal
	// closes underneath them. Ten seconds because an agent mid-turn may be
	// writing a file, and losing that costs far more than a slow shutdown.
	d.stopSupervised(10 * time.Second)

	// Shells get no grace period, and the difference from the line above is the
	// point. A runner may be mid-turn writing a file. A shell is a prompt
	// somebody was reading, and there is nothing in it to lose that ten seconds
	// of waiting would save.
	d.stopShells()

	if n := d.blockedAgents(); n > 0 {
		log.Printf("[atrium] %d agent connection(s) still parked. they will retry against the "+
			"next daemon, and will not wake their model while waiting.", n)
	}

	// Long polls hold their connection until the client goes away, so give
	// them a bounded window rather than waiting for the full poll timeout.
	grace := 5 * time.Second
	if d.opts.LongPoll > 0 && d.opts.LongPoll < grace {
		grace = d.opts.LongPoll
	}
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()

	names := []string{"agent listener", "board listener"}
	done := make(chan string, len(servers))
	for i, s := range servers {
		if s == nil {
			continue
		}
		name := "listener"
		if i < len(names) {
			name = names[i]
		}
		log.Printf("[atrium] closing %s...", name)
		go func(s *http.Server, name string) {
			if err := s.Shutdown(ctx); err != nil {
				log.Printf("[atrium] %s did not close cleanly: %v", name, err)
			}
			done <- name
		}(s, name)
	}

	for range servers {
		select {
		case name := <-done:
			log.Printf("[atrium] %s closed", name)
		case <-ctx.Done():
			log.Printf("[atrium] gave up waiting after %s, forcing the rest closed", grace)
			for _, s := range servers {
				if s != nil {
					_ = s.Close()
				}
			}
			log.Printf("[atrium] stopped in %s", time.Since(start).Round(time.Millisecond))
			return
		}
	}

	// Release the database before Run returns, so the invariant the detached room
	// restarter relies on is explicit rather than incidental. It treats a free
	// --http port as proof the old room and its sqlite handle are gone, and that
	// was true only because the process exits right after Run returns and the OS
	// releases the handle. Closing it here makes it a thing the code does rather
	// than a thing the OS happens to do. See cmd/atrium2/restart.go's
	// waitForRoomRestart. Idempotent, so a caller's deferred Close is still safe.
	//
	// The address file goes first, because finding the shared copy reads the
	// store. See clearLocation.
	d.clearLocation()
	if err := d.closeDB(); err != nil {
		log.Printf("[atrium] closing the database: %v", err)
	}

	log.Printf("[atrium] state is on disk at %s", d.opts.DBPath)
	log.Printf("[atrium] stopped in %s", time.Since(start).Round(time.Millisecond))
}

// blockedAgents counts permission requests currently holding an agent still.
func (d *Daemon) blockedAgents() int {
	pending, err := d.st.PendingPermissions()
	if err != nil {
		return 0
	}
	return len(pending)
}

// SetPRClaim gives the PR door its way to the hub's claim table. Called once the link is built, as SetHubGit is. Nil is
// a room with no hub.
func (d *Daemon) SetPRClaim(f func(ctx context.Context, ask api.PRClaimAsk) (api.PRClaimReply, error)) {
	d.ap.ClaimPR = f
}

// ReconcilePRClaims asks the hub about the PR rows made while it could not be reached. The room's attach calls it.
func (d *Daemon) ReconcilePRClaims() { d.ap.ReconcilePRClaims() }

// resumePRReviews hands the runner the reviews the last daemon was cut in the middle of. Nothing runs at start, so a
// row in fetching or running is one that died with it. Each goes back to queued and is started: the runner reuses the
// steps its folder holds.
func (d *Daemon) resumePRReviews() {
	if d.prr == nil {
		return
	}
	ids, err := d.st.RequeueInterruptedPRs()
	if err != nil {
		log.Printf("[atrium] pr reviews: not resumed: %v", err)
	}
	for _, id := range ids {
		d.prr.Start(id)
	}
}
