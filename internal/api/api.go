// Package api is atrium's human-facing surface: the JSON endpoints and SSE
// stream that the TUI, the SPA, and any future app all consume.
//
// This listener is separate from the agent-facing one. When the store halts,
// the agent listener closes so runners park on connection-refused and stop
// burning tokens, while this one stays up to explain what broke.
package api

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/claudeconf"
	"github.com/dovholuknf/atrium/internal/store"
)

// Server serves the human-facing API.
type Server struct {
	st  *store.Store
	bus *bus
	// Prompt hands a prompt to a waiting agent. Supplied by the daemon, which
	// owns the agent-facing side.
	Prompt func(taskID, text string) error
	// Decide resolves a permission. This must go through the daemon rather
	// than straight to the store, because the agent is blocked on an in-memory
	// reply channel that only the hub can signal. Writing the decision without
	// signalling would leave the runner hanging forever.
	Decide func(permID, decision, reason, command string) (*store.Permission, error)
	// Launch starts a runner. The daemon owns process spawning, so the API
	// hands the request body straight through rather than reaching for it.
	Launch func(body []byte) (*store.Task, error)
	// Kill stops the runner behind a card.
	Kill func(taskID string) error
	// RunAction says a stored action's prompt to a card, and optionally asks
	// the runner to leave afterwards. Owned by the daemon, which owns the
	// terminals and the message queue.
	RunAction http.HandlerFunc
	// RunSource runs one intake source now rather than on its interval, and
	// reports how many new items it produced. Owned by the daemon, which owns
	// process spawning.
	//
	// The error it returns is the source's, not the request's: a source that
	// failed to start is a 200 saying so, because the request worked and the
	// answer is the failure.
	RunSource func(id string) (int, error)
	// Recognise turns a pasted URL into the fields of a launch dialog. Owned by
	// the daemon, which owns process spawning: a recogniser's optional fetch is
	// an operator-written command, and looking at the directory the templates
	// named needs the daemon's filesystem rather than the browser's.
	Recognise func(url string) (*store.Resolved, error)
	// CancelPending answers every outstanding request on a task with a block.
	// Moving a card out of a waiting state has to answer the question rather
	// than hide it, or the agent stays frozen with nobody coming.
	CancelPending func(taskID, reason string) (int, error)
	// Attach upgrades to a WebSocket carrying a supervised runner's terminal.
	// Supplied by the daemon, which owns the processes. Registered only when
	// set, so a build without supervision has no dead route.
	//
	// `?kind=shell` asks for the card's shell instead. Absent means the runner,
	// which is what every caller written before shells existed sends.
	Attach http.HandlerFunc
	// OpenShell starts a plain shell in a card's working directory, beside the
	// runner's own terminal, and ShutShell ends it.
	//
	// The one gap in what the board can do that walking to the machine still
	// answers: when the agent wedges, the terminal on the card belongs to the
	// agent and there is nowhere to type `git status`.
	OpenShell http.HandlerFunc
	ShutShell http.HandlerFunc
	// OlderScrollback serves what this card's terminal held before the last
	// restart, ASKED FOR RATHER THAN PUSHED.
	//
	// It used to be joined onto the front of every attach, and the result was
	// two copies of the last hour: a resumed session reprints its own recent
	// history, so the carried bytes ended mid-conversation and the same
	// conversation began again below the divider. A terminal running claude
	// shows one session, and this is how the older one is reached when it is
	// wanted.
	OlderScrollback http.HandlerFunc
	// DismissAsks takes every outstanding question off a card without telling
	// the session anything.
	//
	// Owned by the daemon rather than answered here, because clearing a
	// question writes to the event log and publishes the card, and both of
	// those belong with the other three ways an ask gets settled.
	DismissAsks http.HandlerFunc
	// Message says something to a running session: typed into its terminal
	// when atrium owns one, queued for the next hook otherwise.
	Message http.HandlerFunc
	// SendNote turns a card's note into one message and clears it. Owned by
	// the daemon, which owns delivery.
	SendNote http.HandlerFunc
	// DrainAuto approves everything already waiting, when auto mode is turned
	// on with a full queue. Supplied by the daemon for the same reason Decide
	// is: each waiting agent is parked on an in-memory reply channel, and a
	// decision written straight to the store never reaches it.
	DrainAuto func() (int, error)
	// Shutdown winds the daemon down. Supplied by the daemon, which is the only
	// thing that can stop itself and owns the access rules for doing so.
	Shutdown http.HandlerFunc
	// Shelve stops the runner behind a card being put down. The card and its
	// resume id stay, so the work is paused rather than ended.
	Shelve func(taskID string) error
	// StopRunner asks a runner to exit the way its harness says to, rather
	// than killing it. Supplied by the daemon, which owns the terminal.
	StopRunner func(taskID string) error
	// Unshelve starts it again from where the conversation left off. Returns
	// whether it started, and why not when it did not, so the board can say so.
	Unshelve func(taskID string) (bool, string, error)

	// Overlays reports how the board can be reached from elsewhere, and turns
	// those ways on and off. Supplied by the daemon, which owns the child
	// process a share runs as.
	Overlays func() any
	// SaveOverlay stores one overlay's configuration.
	SaveOverlay func(kind string, body []byte) error
	// StartOverlay opens a share, StopOverlay closes it.
	StartOverlay func(kind string) error
	StopOverlay  func(kind string) error
	// SetupOverlay does the one thing standing between this machine and being
	// able to share at all: a zrok environment, or a ziti identity. Returns
	// what the tool said, on failure as well as success.
	SetupOverlay func(kind string, body []byte) (string, error)
	// TeardownOverlay undoes that.
	TeardownOverlay func(kind string) (string, error)
	// InspectToken reads an enrollment token without acting on it, so the
	// board can show what it is for before anything is done with it.
	InspectToken func(token string) (any, error)
	// ReserveName holds a zrok name so the board's address survives a restart.
	// Returns the name selection to configure the share with.
	ReserveName func(namespace, name string) (string, error)
	// Capabilities asks a ziti network what this identity may bind, so
	// "is this going to work" is answerable without pressing start.
	Capabilities func() any
	// ZrokAccount reports what the zrok account behind this machine is already
	// using, so an account at a limit is visible before a button is pressed
	// rather than several seconds after. `internal/daemon/overlay_limits.go`
	// has what zrok will and will not tell an account token.
	ZrokAccount func() any
	// SetApiEndpoint points this machine at a zrok instance other than the
	// public one. Empty puts it back on zrok's default.
	SetApiEndpoint func(own bool, endpoint string) error

	// Lending ONE session to one person, which is a different thing from
	// publishing the board and is deliberately not built on it: the board
	// share hands over every card, and this hands over a single terminal.
	ShareCard     func(taskID, mode string) (any, error)
	StopCardShare func(taskID string) error
	GuestShares   func() any

	// BuildExport hands back this atrium's configuration as something a
	// repository can hold, or REFUSES and says why. Refusing is the interesting
	// case: it means something in the configuration looks like a credential,
	// and a secret pushed to a repository is still in its history after it is
	// deleted. See `internal/daemon/export.go`.
	// AuthConfig reports the login in front of the PUBLISHED board, WITHOUT
	// its client secret, and SaveAuth stores one. See `internal/daemon/auth.go`
	// for what this does and does not apply to.
	AuthConfig func() any
	SaveAuth   func(body []byte) error

	// Rooms is the other machines reporting into this hub, and RoomCheckIn is
	// how one does it. Held by the daemon, in memory, because a room's cards
	// belong to that room's database and a durable copy here would be a second
	// source of truth. See `internal/daemon/rooms.go`.
	Rooms       func() any
	RoomCheckIn func(w http.ResponseWriter, r *http.Request)
	// RoomDecide answers a permission request raised on one of those machines.
	// It queues the decision for the room to collect, and nothing more: the
	// blocked channel is in that room's process, so the room unblocks its own
	// request. This is not `Decide`, which resolves a request in this store.
	RoomDecide func(w http.ResponseWriter, r *http.Request)
	// RoomJoin is what to run on another machine to make it a room. Read only,
	// and pressing nothing here creates anything: a room exists exactly when
	// it checks in. RoomForget drops one early, which is the same thing time
	// does and is equally not durable.
	RoomJoin   func() any
	RoomForget func(w http.ResponseWriter, r *http.Request)

	// The other direction: work queued for another machine, which rides the
	// reply to the check-in that room was already making. Unlike the room list
	// this IS durable, because it is this machine's own record of something it
	// asked for rather than a copy of somebody else's card.
	// `DispatchResult` is the room reporting back and carries its own token, so
	// it is not gated on anything this layer knows.
	Dispatches     func() any
	QueueDispatch  func(body []byte) (any, error)
	CancelDispatch func(id string) error
	DispatchResult func(w http.ResponseWriter, r *http.Request)

	BuildExport func() (any, error)
	// ApplyImport reads one back. `apply` false answers what it WOULD do, which
	// is the question somebody restoring a machine actually has, and is the
	// default for that reason.
	ApplyImport func(body []byte, apply, force bool) (any, error)
}

// forever turns a one-off decision into a standing rule, so the same command
// never asks again.
func (s *Server) forever(permID, decision, reason, prefix, kind string) error {
	p, err := s.st.GetPermission(permID)
	if err != nil {
		return err
	}
	if prefix == "" {
		prefix = store.DefaultPrefix(p.Tool, p.Command)
	}
	var addErr error
	if kind == store.KindPath {
		_, addErr = s.st.AddPathRule(p.Tool, prefix, decision, reason, "")
	} else {
		_, addErr = s.st.AddRule(p.Tool, prefix, decision, reason, "")
	}
	if addErr != nil {
		return addErr
	}
	// Recorded against the request so the audit log shows the rule at the
	// moment it was agreed to, not just its existence afterwards.
	return s.st.NoteRuleCreated(permID, prefix)
}

// New builds a server over a store.
func New(st *store.Store) *Server {
	return &Server{st: st, bus: newBus()}
}

// Broadcast publishes a change to every connected client.
func (s *Server) Broadcast(kind string, payload any) { s.bus.publish(kind, payload) }

// Handler returns the mux for the human-facing listener.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /v1/health", s.health)
	mux.HandleFunc("GET /v1/settings", s.getSettings)
	mux.HandleFunc("POST /v1/settings", s.setSettings)
	mux.HandleFunc("GET /v1/fixtures", s.getFixtures)
	mux.HandleFunc("PUT /v1/fixtures/{id}", s.putFixture)
	mux.HandleFunc("POST /v1/fixtures", s.putFixture)
	mux.HandleFunc("DELETE /v1/fixtures/{id}", s.deleteFixture)
	mux.HandleFunc("POST /v1/fixtures/{id}/start", s.startFixture)
	mux.HandleFunc("GET /v1/themes", s.getThemes)
	mux.HandleFunc("PUT /v1/themes/{name}", s.putTheme)
	mux.HandleFunc("DELETE /v1/themes/{name}", s.deleteTheme)
	mux.HandleFunc("POST /v1/themes/import", s.importThemes)
	mux.HandleFunc("GET /v1/overlays", s.getOverlays)
	mux.HandleFunc("PUT /v1/overlays/{kind}", s.putOverlay)
	mux.HandleFunc("POST /v1/overlays/{kind}/start", s.startOverlay)
	mux.HandleFunc("POST /v1/overlays/{kind}/stop", s.stopOverlay)
	mux.HandleFunc("POST /v1/overlays/{kind}/setup", s.setupOverlay)
	mux.HandleFunc("POST /v1/overlays/{kind}/teardown", s.teardownOverlay)
	mux.HandleFunc("POST /v1/overlays/inspect-token", s.inspectToken)
	if s.Rooms != nil {
		mux.HandleFunc("GET /v1/rooms", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, s.Rooms())
		})
	}
	if s.RoomCheckIn != nil {
		mux.HandleFunc("POST /v1/rooms", s.RoomCheckIn)
	}
	if s.RoomDecide != nil {
		mux.HandleFunc("POST /v1/rooms/{room}/permissions/{id}/decide", s.RoomDecide)
	}
	if s.RoomJoin != nil {
		// Before the wildcard, or `join` is read as a room called join. Go's
		// mux prefers the more specific pattern, but stating it here keeps the
		// two lines in the order somebody reads them.
		mux.HandleFunc("GET /v1/rooms/join", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, s.RoomJoin())
		})
	}
	if s.RoomForget != nil {
		mux.HandleFunc("DELETE /v1/rooms/{name}", s.RoomForget)
	}
	if s.Dispatches != nil {
		mux.HandleFunc("GET /v1/dispatch", func(w http.ResponseWriter, r *http.Request) {
			writeJSON(w, http.StatusOK, s.Dispatches())
		})
	}
	if s.QueueDispatch != nil {
		mux.HandleFunc("POST /v1/dispatch", s.queueDispatch)
	}
	if s.CancelDispatch != nil {
		mux.HandleFunc("DELETE /v1/dispatch/{id}", s.cancelDispatch)
	}
	if s.DispatchResult != nil {
		mux.HandleFunc("POST /v1/dispatch/{id}/result", s.DispatchResult)
	}
	mux.HandleFunc("GET /v1/auth", s.getAuth)
	mux.HandleFunc("PUT /v1/auth", s.putAuth)
	mux.HandleFunc("GET /v1/config/export", s.exportConfig)
	mux.HandleFunc("POST /v1/config/import", s.importConfig)
	mux.HandleFunc("GET /v1/shares", s.listGuestShares)
	mux.HandleFunc("POST /v1/tasks/{id}/share", s.shareCard)
	mux.HandleFunc("DELETE /v1/tasks/{id}/share", s.unshareCard)
	if s.ReserveName != nil {
		mux.HandleFunc("POST /v1/overlays/zrok/reserve", s.reserveName)
	}
	if s.SetApiEndpoint != nil {
		mux.HandleFunc("POST /v1/overlays/zrok/endpoint", s.setApiEndpoint)
	}
	if s.Capabilities != nil {
		// A GET that talks to a controller. Slow enough to be worth its own
		// press rather than riding the overlay poll, which runs every time the
		// gear opens.
		mux.HandleFunc("GET /v1/overlays/ziti/services", s.zitiServices)
	}
	if s.ZrokAccount != nil {
		// Same reasoning as the services call above: it talks to somebody
		// else's controller, so it does not ride the overlay poll.
		mux.HandleFunc("GET /v1/overlays/zrok/account", s.zrokAccount)
	}
	mux.HandleFunc("GET /v1/tasks", s.listTasks)
	mux.HandleFunc("GET /v1/tasks/{id}", s.getTask)
	// Every question this card is waiting on, oldest first.
	//
	// The card itself carries only the oldest, which is what a row draws. This
	// is for the dialog, where somebody is reading the card rather than
	// scanning it, and it is the only place the rest of them exist.
	mux.HandleFunc("GET /v1/tasks/{id}/asks", s.listAsks)
	if s.DismissAsks != nil {
		mux.HandleFunc("DELETE /v1/tasks/{id}/asks", s.DismissAsks)
	}
	mux.HandleFunc("PATCH /v1/tasks/{id}", s.patchTask)
	mux.HandleFunc("DELETE /v1/tasks/{id}", s.deleteTask)
	mux.HandleFunc("POST /v1/tasks/prune", s.pruneTasks)
	mux.HandleFunc("POST /v1/intake", s.intake)
	mux.HandleFunc("GET /v1/offered", s.listOffered)
	mux.HandleFunc("GET /v1/history", s.history)
	// Bytes into and out of a card's own directory, and nowhere else. See
	// docs/file-transfer-design.md.
	mux.HandleFunc("POST /v1/tasks/{id}/files", s.uploadFiles)
	mux.HandleFunc("GET /v1/tasks/{id}/files", s.downloadFile)
	// Finding a file so it can be taken back out. Bounded to the card's own
	// directory, and carried by whatever already carries the board.
	mux.HandleFunc("GET /v1/tasks/{id}/files/list", s.listFiles)
	mux.HandleFunc("POST /v1/tasks/{id}/files/open", s.openFile)
	mux.HandleFunc("GET /v1/tasks/{id}/files/zip", s.zipFiles)
	mux.HandleFunc("DELETE /v1/tasks/{id}/files", s.deleteFiles)
	mux.HandleFunc("GET /v1/tasks/{id}/icon", s.getIcon)
	mux.HandleFunc("POST /v1/tasks/{id}/icon", s.putIcon)
	mux.HandleFunc("DELETE /v1/tasks/{id}/icon", s.deleteIcon)
	mux.HandleFunc("GET /v1/tasks/{id}/sessions", s.taskSessions)
	mux.HandleFunc("DELETE /v1/tasks/{id}/sessions/{session}", s.forgetSession)
	mux.HandleFunc("POST /v1/tasks/{id}/files/probe", s.probeFiles)
	mux.HandleFunc("GET /v1/tasks/{id}/files/text", s.readText)
	mux.HandleFunc("PUT /v1/tasks/{id}/files/text", s.writeText)
	mux.HandleFunc("GET /v1/sources", s.listSources)
	mux.HandleFunc("PUT /v1/sources/{id}", s.saveSource)
	mux.HandleFunc("DELETE /v1/sources/{id}", s.deleteSource)
	mux.HandleFunc("POST /v1/sources/{id}/run", s.runSourceNow)
	mux.HandleFunc("GET /v1/recognisers", s.listRecognisers)
	mux.HandleFunc("PUT /v1/recognisers/{id}", s.saveRecogniser)
	mux.HandleFunc("DELETE /v1/recognisers/{id}", s.deleteRecogniser)
	mux.HandleFunc("POST /v1/recognise", s.recognise)
	mux.HandleFunc("GET /v1/browse", s.browse)
	if s.Shutdown != nil {
		mux.HandleFunc("POST /v1/shutdown", s.Shutdown)
	}
	mux.HandleFunc("GET /v1/tasks/{id}/events", s.taskEvents)
	mux.HandleFunc("GET /v1/tasks/{id}/review", s.reviewTask)
	mux.HandleFunc("POST /v1/tasks/{id}/prompt", s.promptTask)
	mux.HandleFunc("GET /v1/waiting", s.waiting)
	mux.HandleFunc("GET /v1/permissions", s.listPermissions)
	mux.HandleFunc("POST /v1/permissions/{id}/decide", s.decidePermission)
	mux.HandleFunc("GET /v1/permissions/history", s.permissionHistory)
	mux.HandleFunc("GET /v1/rules", s.listRules)
	mux.HandleFunc("POST /v1/rules", s.addRule)
	mux.HandleFunc("DELETE /v1/rules/{id}", s.deleteRule)
	mux.HandleFunc("GET /v1/rules/export", s.exportRules)
	mux.HandleFunc("POST /v1/rules/import", s.importRules)
	mux.HandleFunc("GET /v1/rules/preview-claude", s.previewClaudeRules)
	mux.HandleFunc("GET /v1/hooks", s.hookStatus)
	mux.HandleFunc("POST /v1/hooks/install", s.installHooks)
	mux.HandleFunc("GET /v1/harnesses", s.listHarnesses)
	mux.HandleFunc("GET /v1/harnesses/discover", s.discoverRunners)
	mux.HandleFunc("PUT /v1/harnesses/{id}", s.saveHarness)
	mux.HandleFunc("DELETE /v1/harnesses/{id}", s.deleteHarness)
	mux.HandleFunc("POST /v1/launch", s.launch)
	mux.HandleFunc("POST /v1/tasks/{id}/kill", s.kill)
	if s.StopRunner != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/exit", s.exitRunner)
	}
	if s.Attach != nil {
		mux.HandleFunc("GET /v1/tasks/{id}/attach", s.Attach)
	}
	if s.OlderScrollback != nil {
		mux.HandleFunc("GET /v1/tasks/{id}/scrollback/older", s.OlderScrollback)
	}
	if s.OpenShell != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/shell", s.OpenShell)
		mux.HandleFunc("DELETE /v1/tasks/{id}/shell", s.ShutShell)
	}
	mux.HandleFunc("GET /v1/actions", s.listActions)
	mux.HandleFunc("PUT /v1/actions/{id}", s.saveAction)
	mux.HandleFunc("DELETE /v1/actions/{id}", s.deleteAction)
	if s.RunAction != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/action", s.RunAction)
	}
	if s.SendNote != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/note/send", s.SendNote)
	}
	if s.Message != nil {
		mux.HandleFunc("POST /v1/tasks/{id}/message", s.Message)
	}
	// What has been said and not yet arrived. A queued message waits for the
	// session's next tool call or its Stop hook, which can be a while, and a
	// board that does not show the queue makes that look like nothing
	// happened.
	mux.HandleFunc("GET /v1/tasks/{id}/messages", s.pendingMessages)
	mux.HandleFunc("GET /v1/events", s.events)
	mux.Handle("/", webHandler())
	return mux
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("[atrium api] encode: %v", err)
	}
}

func writeErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

// fail maps a store error to a response. A halted store is reported as 503
// with its cause, since explaining that is what this listener is for.
func (s *Server) fail(w http.ResponseWriter, err error) {
	if halted, cause := s.st.Halted(); halted {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{
			"error":  "atrium is halted and will not recover without a restart",
			"cause":  fmt.Sprint(cause),
			"halted": true,
		})
		return
	}
	writeErr(w, http.StatusInternalServerError, err)
}

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	halted, cause := s.st.Halted()
	// `build` is here rather than on its own endpoint because health is the
	// one thing every page already asks, on a timer and on every reconnect.
	body := map[string]any{"ok": !halted, "halted": halted, "build": BuildID}
	if halted {
		body["cause"] = fmt.Sprint(cause)
	}
	writeJSON(w, http.StatusOK, body)
}

// view is a task shaped for a client: observed values already resolved against
// overrides, plus the derived ages the board renders.
type view struct {
	*store.Task
	DisplayTitle string `json:"display_title"`
	IdleSeconds  int64  `json:"idle_seconds"`
	WaitSeconds  int64  `json:"wait_seconds"`
	// Observed marks a session atrium is only watching. The board uses it to
	// offer resume rather than a prompt box, and to stay quiet about it.
	Observed bool `json:"observed"`
	// Supervised marks a runner atrium owns and can therefore attach to. A
	// window mode launch is not supervised, so offering attach on it would be
	// a button that cannot work.
	Supervised bool `json:"supervised"`
	// Shell marks a card that already has a plain shell open beside its runner,
	// so the board can offer to go to it rather than to open a second one.
	//
	// Asked every time rather than remembered in the page. A shell outlives a
	// reload and is closed by an idle sweep the browser knows nothing about, so
	// a board keeping its own list would be wrong within the hour.
	Shell bool `json:"shell,omitempty"`
	// Activity is what the runner is doing right now, as opposed to what it
	// needs. Absent when atrium has heard nothing recently, which is the case
	// for a runner with no hooks and after a daemon restart. See
	// docs/activity-design.md.
	Activity any `json:"activity,omitempty"`
	// Telemetry is how much context the session has burned and how close its
	// account is to a limit, as its statusline last reported. Absent for a
	// runner whose statusline does not post, which is every runner until one
	// is wired up. See docs/statusline-telemetry.md.
	//
	// Its own field rather than part of Activity: the two expire on different
	// clocks, and a session that has been idle for twenty minutes has no
	// activity worth drawing and a context figure that still decides whether
	// you resume it.
	Telemetry any `json:"telemetry,omitempty"`
	// AsksOpen is how many questions this card has outstanding.
	//
	// `Task.Ask` is the OLDEST of them and is what the row draws. That was the
	// whole card until an ask became a row, and it is still the right thing to
	// show first, but on its own it says a card has one question when it may
	// have six. The bug this replaces was a second question DESTROYING the
	// first; drawing one of several without saying so would be the same lie
	// told more quietly.
	//
	// Counted for the whole list in one query rather than per card, the way
	// `UndeliveredCounts` already does for messages. Absent when it is one or
	// zero, because the row says that much by drawing the ask or not.
	AsksOpen int `json:"asks_open,omitempty"`
}

// IsSupervised reports whether atrium owns this task's runner. Supplied by the
// daemon, since the supervisor lives there.
var IsSupervised func(taskID string) bool

// HasShell reports whether this task has a plain shell open beside its runner.
// Supplied by the daemon for the same reason.
var HasShell func(taskID string) bool

// CloseShellFor ends a card's shell, called as the card is deleted.
var CloseShellFor func(taskID string)

// ActivityOf returns a task's live activity, or nil. Supplied by the daemon,
// since the activity is held in memory there rather than in the store.
var ActivityOf func(taskID string) any

// TelemetryOf returns a task's last statusline figure, or nil. Supplied by the
// daemon for the same reason: it is held in memory there and never stored.
var TelemetryOf func(taskID string) any

func toView(t *store.Task) view {
	v := view{
		Task:         t,
		DisplayTitle: t.DisplayTitle(),
		IdleSeconds:  int64(time.Since(t.LastActivityAt).Seconds()),
		Observed:     t.Observed(),
	}
	if IsSupervised != nil {
		v.Supervised = IsSupervised(t.ID)
	}
	if HasShell != nil {
		v.Shell = HasShell(t.ID)
	}
	if ActivityOf != nil {
		v.Activity = ActivityOf(t.ID)
	}
	if TelemetryOf != nil {
		v.Telemetry = TelemetryOf(t.ID)
	}
	if t.WaitingSince != nil {
		v.WaitSeconds = int64(time.Since(*t.WaitingSince).Seconds())
	}
	return v
}

func toViews(ts []*store.Task) []view {
	out := make([]view, 0, len(ts))
	for _, t := range ts {
		out = append(out, toView(t))
	}
	return out
}

// withAskCounts stamps how many questions each card has outstanding.
//
// ONE QUERY FOR THE WHOLE LIST. The alternative is asking per card, which is
// the N+1 this board already avoids for undelivered messages, and the list is
// drawn on every event.
//
// A failure here is swallowed on purpose. The count is a decoration on a row
// whose ask is already drawn from the card itself, so a board that cannot say
// "and two more" is worth serving; a board that answers 500 because a count
// query failed is not.
func (s *Server) withAskCounts(vs []view) []view {
	counts, err := s.st.OpenAskCounts()
	if err != nil {
		return vs
	}
	for i := range vs {
		if vs[i].Task != nil {
			vs[i].AsksOpen = counts[vs[i].Task.ID]
		}
	}
	return vs
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	var statuses []string
	if raw := r.URL.Query().Get("status"); raw != "" {
		statuses = strings.Split(raw, ",")
	}
	tasks, err := s.st.List(statuses...)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.withAskCounts(toViews(tasks))})
}

func (s *Server) waiting(w http.ResponseWriter, r *http.Request) {
	tasks, err := s.st.Waiting()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": s.withAskCounts(toViews(tasks))})
}

// listAsks answers with the questions a card is still waiting on.
//
// Open ones only. The answered ones are in the event log, which is where a
// history belongs; a dialog listing forty settled questions above the one that
// matters is worse than not listing them.
func (s *Server) listAsks(w http.ResponseWriter, r *http.Request) {
	asks, err := s.st.OpenAsks(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	if asks == nil {
		asks = []*store.Ask{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"asks": asks})
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	t, err := s.st.Get(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toView(t))
}

// autoModeReason answers a request already waiting when auto mode is switched
// on. Named, because "approved" alone does not separate a human clicking from a
// mode change sweeping the queue.
const autoModeReason = "auto mode was switched on, so this was approved without being asked"

type patchBody struct {
	Status    *string           `json:"status"`
	Why       *string           `json:"why"`
	Rank      *float64          `json:"rank"`
	Overrides map[string]string `json:"overrides"`
	// AutoApprove turns auto mode on or off for this session.
	AutoApprove *bool `json:"auto_approve"`
	// AutoMinutes is how long to leave it on. Zero or absent means no
	// deadline, which is what this did before deadlines existed.
	AutoMinutes int `json:"auto_minutes"`
	// PeerTyping is whether another session's message goes into this card's
	// terminal or only into its queue. A pointer, so leaving it out is not the
	// same as turning it off.
	PeerTyping *bool `json:"peer_typing"`
	// Tags is the whole set, not an addition. A pointer to a slice so that
	// clearing every tag is distinguishable from not mentioning them.
	Tags *[]string `json:"tags"`
	// Pinned keeps a card at the top of every list and in the terminal
	// switcher whether it is running or not.
	Pinned *bool `json:"pinned"`
	// Theme names the terminal palette. Empty means the board picks one from
	// the project, so clearing it is a meaningful value rather than an
	// omission.
	Theme *string `json:"theme"`
	// Sound names the tone this card rings with. Empty means the board-wide
	// default for whichever kind of alert fired, so clearing it is a
	// meaningful value rather than an omission.
	Sound *string `json:"sound"`
	// Icon is the mark this card wears on a desktop notification. Empty means
	// the atrium mark, so clearing it is a meaningful value rather than an
	// omission, the same as the two above.
	Icon *string `json:"icon"`
	// Where this work came from. Not pointers, because SetOrigin already
	// treats an empty string as "not mentioned": these three are only ever
	// filled in, and unlinking a card from the ticket it came from is not a
	// thing anybody has wanted.
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	URL        string `json:"url"`
	// Prompt is the instruction an offered card carries. A pointer, because
	// clearing it is a decision: a source's default wording is a guess by
	// something that has never read the ticket, and deleting it is a
	// reasonable edit.
	Prompt *string `json:"prompt"`
	// Recap is what the session said it did. Editable, because deciding an
	// account was wrong is the operator's call and read-only text somebody
	// disagrees with is worse than no text. A pointer, so clearing it is a
	// decision rather than an omission.
	Recap *string `json:"recap"`
	// Note is the card's scratch pad. A pointer, because clearing it is a
	// decision and it is also what sending does.
	Note *string `json:"note"`
}

func (s *Server) patchTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var body patchBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	canceled := 0
	// Whether unshelving started the runner again, and why not when it did not.
	// Returned to the caller so the board can say so rather than leaving the
	// operator to notice nothing happened.
	resumed := false
	resumeNote := ""
	if body.Status != nil {
		// Answer anything outstanding before the card moves. A request left
		// pending on a card that is no longer waiting keeps its agent frozen
		// and stays in the queue to be approved later against a situation the
		// operator has already walked away from.
		if *body.Status != store.StatusNeedsPermission && s.CancelPending != nil {
			reason := fmt.Sprintf("the operator moved this task to %s in atrium", *body.Status)
			if *body.Status == store.StatusShelved {
				reason = "this task was shelved in atrium. unshelve it to answer requests from it."
			}
			n, err := s.CancelPending(id, reason)
			if err != nil {
				s.fail(w, err)
				return
			}
			canceled = n
		}
		// Shelving stops the runner. The card, its history and its resume id
		// stay, so unshelving starts the same conversation again. Before the
		// status changes, or the runner's own exit would race it and land the
		// card in dead.
		if *body.Status == store.StatusShelved && s.Shelve != nil {
			if err := s.Shelve(id); err != nil {
				s.fail(w, err)
				return
			}
		}

		was, _ := s.st.Get(id)
		if err := s.st.SetStatus(id, *body.Status); err != nil {
			s.fail(w, err)
			return
		}

		// Coming back off the shelf starts the runner again. After the status
		// change, so a relaunch that fails leaves the card where the operator
		// asked for it rather than stuck in shelved.
		if was != nil && was.Status == store.StatusShelved &&
			*body.Status != store.StatusShelved && s.Unshelve != nil {
			started, why, err := s.Unshelve(id)
			if err != nil {
				s.fail(w, err)
				return
			}
			resumed, resumeNote = started, why
		}
	}
	if body.Why != nil {
		if err := s.st.SetWhy(id, *body.Why); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Rank != nil {
		if err := s.st.SetRank(id, *body.Rank); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Tags != nil {
		if err := s.st.SetTags(id, *body.Tags); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Pinned != nil {
		if err := s.st.SetPinned(id, *body.Pinned); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Theme != nil {
		if err := s.st.SetTheme(id, *body.Theme); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Sound != nil {
		if err := s.st.SetSound(id, *body.Sound); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Icon != nil {
		if err := s.st.SetIcon(id, *body.Icon); err != nil {
			s.fail(w, err)
			return
		}
	}
	if err := s.st.SetOrigin(id, body.Source, body.ExternalID, body.URL); err != nil {
		s.fail(w, err)
		return
	}
	if body.Prompt != nil {
		if err := s.st.SetPrompt(id, *body.Prompt); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Recap != nil {
		if err := s.st.SetRecap(id, *body.Recap); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Note != nil {
		if err := s.st.SetNote(id, *body.Note); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.AutoApprove != nil {
		if body.AutoMinutes < 0 || body.AutoMinutes > maxAutoMinutes {
			writeErr(w, http.StatusBadRequest, fmt.Errorf(
				"auto mode can be left on for up to %d minutes, or with no deadline at all",
				maxAutoMinutes))
			return
		}
		var until *time.Time
		if *body.AutoApprove && body.AutoMinutes > 0 {
			t := time.Now().UTC().Add(time.Duration(body.AutoMinutes) * time.Minute)
			until = &t
		}
		if err := s.st.SetAutoApproveUntil(id, *body.AutoApprove, until); err != nil {
			s.fail(w, err)
			return
		}
		// Turning it on has to answer what is already waiting, or the session
		// stays frozen behind a question nobody will now be asked.
		if *body.AutoApprove && s.Decide != nil {
			pending, err := s.st.PendingForTask(id)
			if err != nil {
				s.fail(w, err)
				return
			}
			for _, p := range pending {
				if _, err := s.Decide(p.ID, "approve", autoModeReason, ""); err != nil {
					s.fail(w, err)
					return
				}
			}
		}
	}
	if body.PeerTyping != nil {
		if err := s.st.SetPeerTyping(id, *body.PeerTyping); err != nil {
			s.fail(w, err)
			return
		}
	}
	if body.Overrides != nil {
		if err := s.st.SetOverrides(id, body.Overrides); err != nil {
			s.fail(w, err)
			return
		}
	}
	t, err := s.st.Get(id)
	if err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("task", toView(t))
	out := toView(t)
	if canceled > 0 {
		s.Broadcast("permission", map[string]any{"canceled": canceled, "task": id})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"task": out, "canceled": canceled,
		"resumed": resumed, "resume_note": resumeNote,
	})
}

func (s *Server) deleteTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	// Before the card goes, not after. A pty holding the card's working
	// directory open once the card is gone is a process nothing on the board
	// accounts for, and on Windows it locks the directory against the removal
	// that is usually the next thing somebody does.
	//
	// The daemon's own sweep catches the bulk paths, where there is no single
	// id to act on. This is here because the operator is watching this one.
	if CloseShellFor != nil {
		CloseShellFor(id)
	}
	if err := s.st.Forget(id); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("task-removed", map[string]string{"id": id})
	w.WriteHeader(http.StatusNoContent)
}

// pruneTasks clears out finished cards in one go. Done and dead only: a sweep
// never touches a shelved card, however long it has sat there.
func (s *Server) pruneTasks(w http.ResponseWriter, r *http.Request) {
	var body struct {
		// OlderThanHours defaults to now, meaning every finished card goes.
		OlderThanHours *float64 `json:"older_than_hours"`
		// Statuses narrows the sweep to one column. Empty means all prunable.
		Statuses []string `json:"statuses"`
	}
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body)
	var age time.Duration
	if body.OlderThanHours != nil && *body.OlderThanHours > 0 {
		age = time.Duration(*body.OlderThanHours * float64(time.Hour))
	}
	n, err := s.st.Prune(age, body.Statuses...)
	if err != nil {
		s.fail(w, err)
		return
	}
	if n > 0 {
		s.Broadcast("tasks-pruned", map[string]int{"removed": n})
	}
	writeJSON(w, http.StatusOK, map[string]int{"removed": n})
}

func (s *Server) taskEvents(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	events, err := s.st.Events(r.PathValue("id"), limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"events": events})
}

// reviewTask answers "what did this session actually do".
//
// The counterpart to auto mode: it folds repeats, groups by tool, and puts the
// decisions nobody saw at the top.
func (s *Server) reviewTask(w http.ResponseWriter, r *http.Request) {
	rev, err := s.st.ReviewTask(r.PathValue("id"))
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rev)
}

func (s *Server) promptTask(w http.ResponseWriter, r *http.Request) {
	if s.Prompt == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no agent transport wired"))
		return
	}
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if err := s.Prompt(r.PathValue("id"), body.Text); err != nil {
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// permView is a request with the session that made it named.
//
// A permission carries a task id, which tells a reader nothing. With several
// sessions running, the same command means different things from the one
// working on the parser and the one deploying.
type permView struct {
	*store.Permission
	Agent    string `json:"agent"`
	Worktree string `json:"worktree,omitempty"`
	// Sound is the asking card's own tone, so an alert about one request can
	// ring as that agent without the board having to hold every task in memory
	// to look it up. The join is already being done here for the name.
	Sound string `json:"sound,omitempty"`
	// Icon rides along for the same reason and on the same join: a
	// notification about a blocked agent should carry that agent's mark, and
	// the alternative is the board holding every task in memory to look one up.
	Icon string `json:"icon,omitempty"`
}

// namePermissions attaches the asking session to each request.
//
// One pass over the tasks rather than a lookup per permission: the decisions
// log runs to hundreds of rows and this paints it.
func (s *Server) namePermissions(perms []*store.Permission) []permView {
	out := make([]permView, 0, len(perms))
	if len(perms) == 0 {
		return out
	}
	byID := map[string]*store.Task{}
	if tasks, err := s.st.List(); err == nil {
		for _, t := range tasks {
			byID[t.ID] = t
		}
	}
	for _, p := range perms {
		v := permView{Permission: p}
		if t := byID[p.TaskID]; t != nil {
			v.Agent = t.DisplayTitle()
			v.Worktree = t.Worktree
			v.Sound = t.Sound
			v.Icon = t.Icon
		}
		out = append(out, v)
	}
	return out
}

func (s *Server) listPermissions(w http.ResponseWriter, r *http.Request) {
	perms, err := s.st.PendingPermissions()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": s.namePermissions(perms)})
}

func (s *Server) decidePermission(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		Forever  bool   `json:"forever"`
		Prefix   string `json:"prefix"`
		Command  string `json:"command"`
		// Kind is how Prefix should be read: a command shape, or a folder that
		// covers everything inside it. Empty means command, which is what
		// every caller sent before folders existed.
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Decision != "approve" && body.Decision != "block" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decision must be approve or block"))
		return
	}
	// The rule is written before the decision so a crash in between leaves the
	// request pending rather than leaving a rule nobody agreed to.
	if body.Forever {
		if err := s.forever(r.PathValue("id"), body.Decision, body.Reason,
			body.Prefix, body.Kind); err != nil {
			// A rejected pattern is the caller's problem, not a server fault,
			// so it must not be reported as one. Only a halted store gets to
			// take the 5xx path here.
			if halted, _ := s.st.Halted(); halted {
				s.fail(w, err)
				return
			}
			writeErr(w, http.StatusBadRequest, err)
			return
		}
	}
	if s.Decide == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no agent transport wired"))
		return
	}
	// Answering something already answered silently returns the original
	// decision, which from a notification button looks like nothing happened.
	// Say so instead, and say what the answer was.
	if existing, err := s.st.GetPermission(r.PathValue("id")); err == nil && existing.DecidedAt != nil {
		by := existing.DecidedBy
		if by == "" || by == store.DecidedBySelf {
			by = "you"
		} else {
			by = "the rule " + by
		}
		// "approve" plus "ed" is "approveed". Past tense is a lookup, not a
		// suffix.
		past := map[string]string{"approve": "approved", "block": "blocked"}[existing.Decision]
		if past == "" {
			past = existing.Decision
		}
		writeJSON(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("already %s by %s at %s",
				past, by, existing.DecidedAt.Format("15:04:05")),
			"already":  true,
			"decision": existing.Decision,
		})
		return
	}
	p, err := s.Decide(r.PathValue("id"), body.Decision, body.Reason, body.Command)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) permissionHistory(w http.ResponseWriter, r *http.Request) {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	perms, err := s.st.History(limit)
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"permissions": s.namePermissions(perms)})
}

// addRule writes a standing answer by hand.
//
// A rule could previously only be born from a request just read, through always
// or never. That misses the case where the answer is already known: "this tool
// may work under this folder". Waiting to be asked once per command shape so
// you can press always is not a workflow.
func (s *Server) addRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Tool     string `json:"tool"`
		Prefix   string `json:"prefix"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
		// Scope narrows a rule to one worktree. Empty means every session.
		//
		// It changes which requests a rule is even considered for, so a caller
		// that omits it gets a rule that applies everywhere. That is the right
		// default for a hand-written rule, and it is stated here because
		// getting it wrong silently produces a rule that never fires.
		Scope string `json:"scope"`
		// Kind is "command" (a prefix or glob) or "path" (a directory).
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Tool) == "" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("a rule needs a tool"))
		return
	}
	if body.Decision != "approve" && body.Decision != "block" {
		writeErr(w, http.StatusBadRequest, fmt.Errorf("decision must be approve or block"))
		return
	}

	var (
		rule *store.Rule
		err  error
	)
	if body.Kind == store.KindPath {
		rule, err = s.st.AddPathRule(body.Tool, body.Prefix, body.Decision, body.Reason, body.Scope)
	} else {
		rule, err = s.st.AddRule(body.Tool, body.Prefix, body.Decision, body.Reason, body.Scope)
	}
	if err != nil {
		// A refused pattern is the caller's to fix, not a server fault.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("rule", rule)
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) listRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.Rules()
	if err != nil {
		s.fail(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rules": rules})
}

func (s *Server) deleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteRule(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("rules", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

// exportRules writes every standing rule as JSON that importRules accepts, so
// a rule set can be moved between machines or kept in a file.
func (s *Server) exportRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.st.Rules()
	if err != nil {
		s.fail(w, err)
		return
	}
	out := make([]claudeconf.Entry, 0, len(rules))
	for _, rule := range rules {
		out = append(out, claudeconf.Entry{
			Tool: rule.Tool, Pattern: rule.Prefix, Decision: rule.Decision, Source: "atrium",
		})
	}
	w.Header().Set("Content-Disposition", `attachment; filename="atrium-rules.json"`)
	writeJSON(w, http.StatusOK, map[string]any{"rules": out})
}

// previewClaudeRules shows what importing from Claude Code's settings would
// do, without doing it. Import is not something to run blind.
func (s *Server) previewClaudeRules(w http.ResponseWriter, r *http.Request) {
	dir := r.URL.Query().Get("dir")
	entries, skipped, err := claudeconf.Load(dir)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"rules":   entries,
		"skipped": skipped,
		"sources": claudeconf.SettingsPaths(dir),
	})
}

// importRules adds rules from Claude Code's settings, or from a JSON body that
// exportRules produced.
func (s *Server) importRules(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Source     string             `json:"source"` // "claude" or "json"
		Dir        string             `json:"dir"`
		Rules      []claudeconf.Entry `json:"rules"`
		IncludeAll bool               `json:"include_broad"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}

	entries := body.Rules
	var skipped []claudeconf.Skipped
	if body.Source == "claude" {
		var err error
		entries, skipped, err = claudeconf.Load(body.Dir)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err)
			return
		}
	}

	added, updated := 0, 0
	var failed []map[string]string
	for _, e := range entries {
		if e.Broad && !body.IncludeAll {
			skipped = append(skipped, claudeconf.Skipped{
				Raw:    e.Tool + " " + e.Pattern,
				Source: e.Source,
				Reason: "matches every request for this tool, not imported unless you ask for it",
			})
			continue
		}
		existing, err := s.st.Rules()
		if err != nil {
			s.fail(w, err)
			return
		}
		had := false
		for _, r := range existing {
			if r.Tool == e.Tool && r.Prefix == e.Pattern {
				had = true
				break
			}
		}
		add := s.st.AddRule
		if e.Broad {
			add = s.st.AddBroadRule
		}
		if _, err := add(e.Tool, e.Pattern, e.Decision, "imported from "+e.Source, ""); err != nil {
			failed = append(failed, map[string]string{
				"tool": e.Tool, "pattern": e.Pattern, "error": err.Error(),
			})
			continue
		}
		if had {
			updated++
		} else {
			added++
		}
	}
	s.Broadcast("rules", map[string]int{"added": added})
	writeJSON(w, http.StatusOK, map[string]any{
		"added": added, "updated": updated, "skipped": skipped, "failed": failed,
	})
}

func (s *Server) saveHarness(w http.ResponseWriter, r *http.Request) {
	var h store.Harness
	if err := json.NewDecoder(r.Body).Decode(&h); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	h.ID = r.PathValue("id")
	saved, err := s.st.SaveHarness(h)
	if err != nil {
		if halted, _ := s.st.Halted(); halted {
			s.fail(w, err)
			return
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("harnesses", saved)
	writeJSON(w, http.StatusOK, saved)
}

func (s *Server) deleteHarness(w http.ResponseWriter, r *http.Request) {
	if err := s.st.DeleteHarness(r.PathValue("id")); err != nil {
		s.fail(w, err)
		return
	}
	s.Broadcast("harnesses", map[string]string{"removed": r.PathValue("id")})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) launch(w http.ResponseWriter, r *http.Request) {
	if s.Launch == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no launcher wired"))
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	task, err := s.Launch(body)
	if err != nil {
		// A bad harness, a missing directory or a mode that is not built are
		// all the caller's problem, not a server fault.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	s.Broadcast("task", toView(task))
	writeJSON(w, http.StatusOK, toView(task))
}

// queueDispatch files a launch for another machine.
//
// A bad room name, a missing runner id or a queue that is already full are all
// the caller's problem, so they come back as 400 the way `launch` does. Nothing
// here is checked against THIS machine's harnesses: see
// `daemon.QueueDispatch`.
func (s *Server) queueDispatch(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	item, err := s.QueueDispatch(body)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// cancelDispatch withdraws an item, and refuses once a room has taken it.
func (s *Server) cancelDispatch(w http.ResponseWriter, r *http.Request) {
	if err := s.CancelDispatch(r.PathValue("id")); err != nil {
		// 409 rather than 400: the request was fine and the world moved. The
		// board draws the message, which says to stop it on that machine.
		writeErr(w, http.StatusConflict, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) kill(w http.ResponseWriter, r *http.Request) {
	if s.Kill == nil {
		writeErr(w, http.StatusNotImplemented, fmt.Errorf("no launcher wired"))
		return
	}
	if err := s.Kill(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// exitRunner asks a runner to quit the way its own harness says to, rather
// than killing it. `kill` remains for when asking does not work.
func (s *Server) exitRunner(w http.ResponseWriter, r *http.Request) {
	if err := s.StopRunner(r.PathValue("id")); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// events is the SSE stream every live client subscribes to.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeErr(w, http.StatusInternalServerError, fmt.Errorf("streaming unsupported"))
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	sub := s.bus.subscribe()
	defer s.bus.unsubscribe(sub)

	ping := time.NewTicker(25 * time.Second)
	defer ping.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg, ok := <-sub:
			// A closed channel means the daemon is shutting down. Returning
			// releases the request, which is what lets the listener close
			// instead of waiting out an open stream.
			if !ok {
				return
			}
			fmt.Fprintf(w, "event: %s\ndata: %s\n\n", msg.kind, msg.data)
			flusher.Flush()
		case <-ping.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// Close releases every SSE subscriber. An event stream is an open request, and
// http.Server.Shutdown waits for those, so without this a single browser tab
// holds the board listener open until the shutdown grace period expires.
func (s *Server) Close() { s.bus.close() }

type message struct {
	kind string
	data []byte
}

// bus is a tiny fan-out for SSE subscribers. Slow subscribers are dropped
// rather than allowed to block a publisher.
type bus struct {
	mu     sync.Mutex
	subs   map[chan message]struct{}
	closed bool
}

func newBus() *bus { return &bus{subs: map[chan message]struct{}{}} }

func (b *bus) subscribe() chan message {
	ch := make(chan message, 32)
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		// Already shutting down. Hand back a closed channel so the caller
		// returns immediately rather than parking on a stream nobody feeds.
		close(ch)
		return ch
	}
	b.subs[ch] = struct{}{}
	return ch
}

func (b *bus) unsubscribe(ch chan message) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if _, ok := b.subs[ch]; ok {
		delete(b.subs, ch)
	}
}

// close releases every subscriber exactly once.
func (b *bus) close() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return
	}
	b.closed = true
	for ch := range b.subs {
		close(ch)
		delete(b.subs, ch)
	}
}

func (b *bus) publish(kind string, payload any) {
	data, err := json.Marshal(payload)
	if err != nil {
		log.Printf("[atrium api] publish %s: %v", kind, err)
		return
	}
	msg := message{kind: kind, data: data}
	b.mu.Lock()
	defer b.mu.Unlock()
	for ch := range b.subs {
		select {
		case ch <- msg:
		default:
		}
	}
}

// pendingMessages lists what has been said to a card and has not arrived yet.
//
// A queued message waits for the session's next tool call or its Stop hook,
// which can be minutes. Without this the board has nothing to show between
// sending and delivery, and the send looks like it did nothing.
func (s *Server) pendingMessages(w http.ResponseWriter, r *http.Request) {
	msgs, err := s.st.PendingMessages(r.PathValue("id"))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}
