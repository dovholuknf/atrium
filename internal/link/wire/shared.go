package wire

import (
	"encoding/json"
	"net/http"
	"time"
)

// Room reports what is attached under a name.
type Attached struct {
	Name    string `json:"name"`
	Version string `json:"version,omitempty"`
	Host    string `json:"host,omitempty"`
	// Commit is the commit the room said it runs. Empty for a room that does not say.
	Commit string    `json:"commit,omitempty"`
	Since  time.Time `json:"since"`
	Idle   int       `json:"idle"`
	Beat   time.Time `json:"last_beat"`
	OS     string    `json:"os,omitempty"`
	Arch   string    `json:"arch,omitempty"`
	// Tools is where the room said each toolchain is. Nil from a room that does not say.
	Tools map[string]string `json:"tools,omitempty"`
	// Proven is false for a room that attached over an overlay WITHOUT a
	// certificate, so what it is called is only what it said. That is how an
	// operator finds who must re-join before the old path is refused.
	Proven bool `json:"proven"`
	// Git is whether the room said it takes git syncs.
	Git bool `json:"git,omitempty"`
	// Local is a room on the hub's own machine, which the merge queue tests on when no merge room is set.
	Local bool `json:"local,omitempty"`
	// IdleCPU is the idle percent the room last reported on a beat. Nil for a room that does not send one.
	IdleCPU *float64 `json:"idle_cpu,omitempty"`
	// Setup is what the hub last read of this room's setup, nil before the first read. Filled by the proxy, never by
	// the room. See setupwatch.go.
	Setup *RoomSetup `json:"setup,omitempty"`
	// Via is the transport this room is attached over, from the listener that took
	// it. Empty from a hub serving one unnamed listener. Fallback is the room saying
	// it is not on its first choice. See docs/rnd/room-transport-order.md.
	Via      string `json:"via,omitempty"`
	Fallback bool   `json:"fallback,omitempty"`
	// Starved is a room whose control connection is up while no data connection
	// has arrived for Timings.Starve: attached in name, unreachable from the
	// board. Its data dials are going somewhere the hub is not.
	Starved bool `json:"starved,omitempty"`
	// RemoteAccess is what the room said about its ssh switch. Nil is a room that never says, which reads as unknown.
	// Observed, never trusted for granting, and the hub cannot change it. See stream.go.
	RemoteAccess *RemoteAccess `json:"remote_access,omitempty"`
	// LoginNeeded is why the room says its claude login needs doing, empty when it does not say or it is fine.
	// Observed from the beat and never acted on here. See internal/daemon/loginneeded.go.
	LoginNeeded string `json:"login_needed,omitempty"`
	// Paths is how each of the room's rungs looked from the room on its last beat, nil from a room that does not say.
	Paths []PathState `json:"paths,omitempty"`
}

// RoomSetup is what the hub last learned about one room's setup.
type RoomSetup struct {
	// Answering is false when the room did not answer the check. Nothing else is meaningful then.
	Answering bool      `json:"answering"`
	Checked   time.Time `json:"checked"`
	// HooksMissing is the report's `missing` count, which includes stale hooks. HooksStale counts those alone.
	HooksMissing int `json:"hooks_missing"`
	HooksStale   int `json:"hooks_stale"`
	// HooksUnreadable is the parse error when settings.json cannot be read. Nothing is written then, so no fix.
	HooksUnreadable string `json:"hooks_unreadable,omitempty"`
	// HooksTwoGates is true when the dotfiles gate and atrium's own are both registered. It is one of HooksMissing.
	HooksTwoGates bool   `json:"hooks_two_gates,omitempty"`
	Build         string `json:"build,omitempty"`
	// Code is the room's code fingerprint and Commit the commit it was built from. Both are empty on an old room.
	Code    string   `json:"code,omitempty"`
	Commit  string   `json:"commit,omitempty"`
	Go      string   `json:"go,omitempty"`
	Runners []string `json:"runners,omitempty"`
	// Issues is what is wrong, named once here so the pill and the row cannot disagree.
	Issues []SetupIssue `json:"issues"`

	Since time.Time `json:"-"`
}

// SetupIssue is one thing wrong with a room, in a line a person can read.
type SetupIssue struct {
	// Kind is `hooks` or `build`. Only `hooks` has a fix on the board.
	Kind string `json:"kind"`
	Text string `json:"text"`
}

// RelayRequest is what a room asks its hub to carry.
type RelayRequest struct {
	Op string `json:"op"`
	// From is the sender's handle ON THE ASKING ROOM. The hub adds the room from
	// the connection, never from here, so a room cannot speak for another.
	From string `json:"from,omitempty"`
	// Room and To are the target: a room other than the asking one, and a
	// handle, alias or card id on it.
	Room string `json:"room,omitempty"`
	To   string `json:"to,omitempty"`
	Text string `json:"text,omitempty"`
	When string `json:"when,omitempty"`
	// Kind is `fyi` or empty, for `say`: a launcher's fyi makes no report owed on
	// the target room. An older hub ignores it and the say goes as `needs`.
	Kind string `json:"kind,omitempty"`
	// Wake resumes a parked target before the message is delivered, for `say`. An older hub
	// ignores it, and the target answers `parked` as it always did.
	Wake bool `json:"wake,omitempty"`
	// All includes cards with no session, for `peers`.
	All bool `json:"all,omitempty"`
	// Everywhere asks `peers` for the cards tagged atrium:everywhere on other
	// rooms and nothing else. An older hub ignores it.
	Everywhere bool `json:"everywhere,omitempty"`
	// Events includes the card's recent events, for `card`.
	Events bool `json:"events,omitempty"`
	// Launch is the session to start, for `launch`. An older hub ignores it and
	// refuses the op by name.
	Launch *RelayLaunchSpec `json:"launch,omitempty"`
	// Force, for `exit`, is the asker overriding the guard on a card that is
	// neither theirs nor their own. The room that owns the card records it.
	Force bool `json:"force,omitempty"`
	// Method, Path and Body are the hub call, for `hub`. Only /_hub/backlog and /_hub/reports are served.
	Method string          `json:"method,omitempty"`
	Path   string          `json:"path,omitempty"`
	Body   json.RawMessage `json:"body,omitempty"`
}

// RelayLaunchSpec is one launch for another room, field for field what the hub's own
// atrium_launch takes beside `room`. Room is the target, From (on the request) the
// launcher on the asking room.
type RelayLaunchSpec struct {
	Cwd        string            `json:"cwd"`
	Title      string            `json:"title,omitempty"`
	Why        string            `json:"why,omitempty"`
	Prompt     string            `json:"prompt,omitempty"`
	Brief      string            `json:"brief,omitempty"`
	Runner     string            `json:"runner,omitempty"`
	Tags       []string          `json:"tags,omitempty"`
	Model      string            `json:"model,omitempty"`
	Effort     string            `json:"effort,omitempty"`
	Args       []string          `json:"args,omitempty"`
	Env        map[string]string `json:"env,omitempty"`
	LeanAgents []string          `json:"lean_agents,omitempty"`
	LeanSkills []string          `json:"lean_skills,omitempty"`
	Superlean  bool              `json:"superlean,omitempty"`
	Lifetime   string            `json:"lifetime,omitempty"`
	Item       bool              `json:"item,omitempty"`
}

type ErrStatus int

func (e ErrStatus) Error() string { return "the room answered " + http.StatusText(int(e)) }

// Event is one thing a room said.
type Event struct {
	Room string
	Kind string
	Data []byte
}

// DocsCrossOrigin is the one cross-origin check every hub write route shares.
var DocsCrossOrigin = http.NewCrossOriginProtection()

// KeyOf is the key a room's name is kept under.
func KeyOf(s string) string { return LowerASCII(s) }

// RelayAnswer is the hub's answer.
type RelayAnswer struct {
	OK bool `json:"ok"`
	// Code and Error are a refusal, in the target room's words where it gave
	// some. Unreachable marks one the sender's room may hold and try again,
	// rather than one that will never succeed.
	Code        int    `json:"code,omitempty"`
	Error       string `json:"error,omitempty"`
	Unreachable bool   `json:"unreachable,omitempty"`
	// Unconfirmed is a failure AFTER the message may have reached the target,
	// so sending it again could deliver it twice. Never held for a say.
	Unconfirmed bool `json:"unconfirmed,omitempty"`
	// What the target room said about a delivered message.
	Delivered string `json:"delivered,omitempty"`
	When      string `json:"when,omitempty"`
	Warning   string `json:"warning,omitempty"`
	// To is the resolved `handle@room` and Card the tagged `room~id`.
	To    string      `json:"to,omitempty"`
	Card  string      `json:"card,omitempty"`
	Peers []RelayPeer `json:"peers,omitempty"`
	// Task is the card `card` read, named across, or the one `launch` started.
	Task *RelayTask `json:"task,omitempty"`
	// What `launch` started: where to watch it, where its briefing was written on the
	// target's disk, and the model and effort the room says it ran with.
	Watch  string `json:"watch,omitempty"`
	Brief  string `json:"brief,omitempty"`
	Model  string `json:"model,omitempty"`
	Effort string `json:"effort,omitempty"`
	// Data is the hub endpoint's own JSON answer, for `hub`.
	Data json.RawMessage `json:"data,omitempty"`
}

// RelayTask is one card on another room, as atrium_task reports it. Card is
// `room~id` and Handle `name@room`.
type RelayTask struct {
	Card    string       `json:"card"`
	Handle  string       `json:"handle,omitempty"`
	Title   string       `json:"title,omitempty"`
	Status  string       `json:"status"`
	Doing   string       `json:"doing,omitempty"`
	Where   string       `json:"where,omitempty"`
	Why     string       `json:"why,omitempty"`
	Idle    int          `json:"idle_seconds,omitempty"`
	Waiting int          `json:"waiting_seconds,omitempty"`
	Owned   bool         `json:"atrium_owns_terminal"`
	Events  []RelayEvent `json:"events,omitempty"`
}

// RelayEvent is one event on a RelayTask.
type RelayEvent struct {
	At   string `json:"at"`
	Kind string `json:"kind"`
}

// RelayPeer is one session on another room.
type RelayPeer struct {
	Handle  string `json:"handle"`
	Alias   string `json:"alias,omitempty"`
	Card    string `json:"card"`
	Room    string `json:"room"`
	Title   string `json:"title,omitempty"`
	Status  string `json:"status"`
	Doing   string `json:"doing,omitempty"`
	Where   string `json:"where,omitempty"`
	Waiting int    `json:"waiting_seconds,omitempty"`
	Owned   bool   `json:"atrium_owns_terminal"`
	// Everywhere marks a card that is here because it carries atrium:everywhere.
	Everywhere bool `json:"everywhere,omitempty"`
}
