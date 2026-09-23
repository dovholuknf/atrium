// Package store is atrium's durable state: tasks, their event history, and
// pending permission requests.
//
// The failure posture documented in docs/architecture-v2.md lives here. There
// is no degraded mode. Failures sort into three tiers:
//
//  1. Open or migration failure. Open returns an error and the daemon refuses
//     to start.
//  2. Contention (SQLITE_BUSY, SQLITE_LOCKED). Not a failure. Retried
//     internally and never surfaced.
//  3. Any other failure. The store halts: it records the cause, refuses all
//     further work, and calls OnHalt so the daemon can close the agent-facing
//     listener. To an agent a closed listener reads as connection-refused,
//     the one failure its client already absorbs silently, so every session
//     parks instead of burning tokens.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// TimeFormat is how every timestamp column is written. RFC3339 in UTC with
// milliseconds sorts lexicographically, which is what lets the same column
// work on SQLite and Postgres without a real timestamp type.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// Status values. These are the kanban columns.
const (
	StatusBacklog         = "backlog"
	StatusRunning         = "running"
	StatusNeedsInput      = "needs-input"
	StatusNeedsPermission = "needs-permission"
	StatusDone            = "done"
	StatusShelved         = "shelved"
	StatusDead            = "dead"
)

// Why a card is waiting. Empty is the default and means a turn ended, so every
// card written before this existed reads correctly without a backfill.
const (
	// WaitingStarted is a session that has only just come up. It is ready
	// because it has not done anything yet, which is the opposite of the other
	// way into this column and has to read that way.
	WaitingStarted = "started"
	// WaitingAsked is a session that put a QUESTION to you, rather than one
	// that ran out of things to do.
	//
	// Knowable because asking is a tool call, so the permission hook sees it
	// by name before the turn ends. Nothing in the Stop hook could have told
	// these apart: "the turn ended" is all it says.
	//
	// The difference is worth a column's worth of attention. A card that asked
	// has a person on the other end of it who cannot proceed; a card that
	// merely finished does not.
	WaitingAsked = "asked"
)

// Event kinds.
const (
	EventCreated       = "created"
	EventSubmitted     = "submitted"
	EventPrompted      = "prompted"
	EventPermRequested = "perm-requested"
	EventPermDecided   = "perm-decided"
	EventStatusChanged = "status-changed"
	EventNotified      = "notified"
	EventLaunched      = "launched"
	EventExited        = "exited"
	// EventCompacted is a session forgetting: Claude Code's PreCompact hook,
	// which fires as a conversation is about to be summarised into a shorter
	// one.
	//
	// A moment rather than a state, so there is no status and no lane. What it
	// answers is why an agent stopped knowing something it clearly knew an
	// hour ago, which is a question that comes up on its own.
	//
	// What is deliberately NOT recorded is what was lost. That would mean
	// reading the transcript, and a per-agent transcript is out of scope.
	EventCompacted = "compacted"
)

// ErrHalted is returned by every store call once the store has halted.
var ErrHalted = errors.New("store is halted")

// ErrClosed is returned by every store call after Close. A call that arrives
// once the daemon has released the store on its way down is late, not a
// failure, and halting over it would report a clean stop as a broken database.
var ErrClosed = errors.New("store is closed")

// Task is one card on the board.
type Task struct {
	ID             string     `json:"id"`
	Title          string     `json:"title"`
	Why            string     `json:"why"`
	Repo           string     `json:"repo"`
	Worktree       string     `json:"worktree"`
	Runner         string     `json:"runner"`
	Hostname       string     `json:"hostname"`
	PID            int        `json:"pid"`
	Status         string     `json:"status"`
	CreatedAt      time.Time  `json:"created_at"`
	LastActivityAt time.Time  `json:"last_activity_at"`
	WaitingSince   *time.Time `json:"waiting_since,omitempty"`
	// WaitingReason is why this card is in a waiting column, which is a
	// different question from how long it has been there.
	//
	// `needs-input` is reached two ways that mean opposite things: a session
	// that has just started is ready because it has done nothing yet, and one
	// that finished a turn is ready because it did what was asked. Empty means
	// a turn ended, which is what every card written before this column
	// existed was.
	WaitingReason string            `json:"waiting_reason,omitempty"`
	WireName      string            `json:"wire_name"`
	Overrides     map[string]string `json:"overrides"`
	Rank          float64           `json:"rank"`
	// ExternalID ties a card to a session atrium did not start, using the
	// identifier its owner already uses. ResumeID is what the runner needs to
	// pick that conversation back up.
	ExternalID string `json:"external_id,omitempty"`
	ResumeID   string `json:"resume_id,omitempty"`
	// Source names the system ExternalID belongs to, and URL is the way back
	// to the thing itself.
	//
	// Atrium never learns what a source means. `github`, `zendesk` and `ci`
	// are strings it renders as a badge and stores, and whoever posted the
	// item did the reading. That is the whole reason intake can serve a system
	// nobody has thought of yet. See docs/intake-design.md.
	Source string `json:"source,omitempty"`
	URL    string `json:"url,omitempty"`
	// Prompt is the instruction this card was raised with, waiting for a
	// runner to hand it to.
	//
	// An offered card has no session, so there is nothing to say it to yet.
	// Kept after the start rather than cleared: it is what this card is for,
	// and a start that failed should be repeatable without retyping it.
	Prompt string `json:"prompt,omitempty"`
	// IntakeKey deduplicates work raised by a source. Empty on every card a
	// human made or a script launched.
	//
	// Held as its own column rather than enforced over source and external id,
	// so that uniqueness applies to a poller and not to a person. Two poll
	// ticks reporting one ticket are one card; two deliberate launches naming
	// one ticket are two pieces of work somebody asked for twice.
	IntakeKey string `json:"intake_key,omitempty"`
	Branch    string `json:"branch,omitempty"`
	// Model is which model this card was launched on, if one was named.
	//
	// ONE TIME WITH RESPECT TO THE HARNESS, STICKY WITH RESPECT TO THE CARD.
	// The form forgets the choice the moment it is made, so nobody has to
	// remember to turn it back off. The card does not: a session that started
	// on a model stays on it, and `reopen.go` replays this after a restart. A
	// model not written down here is a session quietly moved back to the
	// default the next time the daemon comes up, which is the failure this
	// field exists to prevent.
	//
	// Empty means nothing was chosen and the runner's own default applies.
	// Atrium never learns what a model name means, exactly as it never learns
	// what a source is: it is text the operator typed, handed to the runner in
	// the shape that runner declared.
	Model string `json:"model,omitempty"`
	// LastCols is how wide this card's terminal was when it was last stopped.
	//
	// Written at the wind-down, never on the resize itself. A browser sends a
	// resize frame whenever anything on the page moves, and that is not a rate
	// to write to a database at.
	//
	// It exists so a reopened terminal comes up the size of the window it is
	// about to appear in. Opening at a fixed width and being resized a moment
	// later put a stretch of output composed for nobody into the scrollback on
	// every restart, with hard line breaks a third of the way across. Zero for
	// a card that has never had a terminal atrium owned.
	LastCols int `json:"last_cols,omitempty"`
	// PeerTyping is whether another session may type into this card's
	// terminal, rather than only queue for it. On by default.
	//
	// NOT omitempty, because false is the interesting value here and a field
	// that vanishes when it is off is a field the board cannot draw a switch
	// from.
	PeerTyping bool `json:"peer_typing"`
	// Org and Host are which one, above the repo name.
	//
	// `openziti/zrok` on `github.com` and a fork of it under another org are
	// the same `Repo`, and a board that groups on the name alone puts them in
	// one pile. Filled in by a launcher that resolved a URL and empty for a
	// session that joined on its own, which is the posture every observed
	// field here takes.
	Org  string `json:"org,omitempty"`
	Host string `json:"host,omitempty"`
	// WindowName is which pile of work this card belongs to.
	//
	// The board's grouping key, and it exists because "what repo" is not the
	// only question somebody sorts by. A pull request, a tangent, a support
	// thread and the main line of work in one repo are four different piles,
	// and only the last of them is answered by the path.
	//
	// Free text, like Tags and for the same reason: a fixed list would be
	// atrium deciding what kinds of work exist.
	WindowName string `json:"window_name,omitempty"`
	// Gated is whether this session has joined atrium. It is state rather than
	// an environment variable so a running session can opt in or out without
	// being restarted.
	Gated bool `json:"gated"`
	// AutoApprove answers this session's requests with approve instead of
	// asking. Everything is still recorded, and the audit log is read
	// afterwards to see what was let through.
	//
	// It does not override a standing never rule or a shelved card: auto mode
	// stops new questions, it does not discard answers already given.
	AutoApprove bool `json:"auto_approve"`
	// AutoUntil is when auto mode stops for this card, or nil for no deadline.
	//
	// Read at the moment a decision is made rather than enforced by a timer. A
	// timer that has to fire is a timer that does not fire across a restart,
	// and auto mode surviving a restart it should not have survived is the
	// failure worth designing against.
	AutoUntil *time.Time `json:"auto_until,omitempty"`
	// Tags are what the operator calls this card, as opposed to what atrium
	// worked out from its path. Grouping already derives a project from the
	// worktree, which answers "what repo" and nothing else. A card is also a
	// support case, a tangent, a pull request, a lab, and none of that is in
	// the path.
	//
	// Free text on purpose. A fixed list would be atrium deciding what kinds
	// of work exist.
	Tags []string `json:"tags"`
	// Throwaway marks a card whose temporary directory, card, and transcripts
	// are removed when the session ends. PromoteTo records a permanent destination
	// instead. The move waits until exit so the working directory is no longer
	// in use. An empty destination leaves cleanup enabled.
	Throwaway bool   `json:"throwaway,omitempty"`
	PromoteTo string `json:"promote_to,omitempty"`
	// Pinned keeps a card at the top of every list and always in the terminal
	// switcher. Some sessions are permanent fixtures and hunting for them in
	// activity order is the wrong shape.
	//
	// A pinned card stays in the terminal switcher AFTER ITS RUNNER EXITS,
	// greyed rather than gone. That is what makes the pinned set a bucket you
	// keep things in instead of a filter over what happens to be running: a
	// row that vanishes when you quit the session is a row you have to put
	// back, and putting it back is the work pinning it was meant to save.
	Pinned bool `json:"pinned"`
	// PinOrder is where this card sits among the other pinned ones, smallest
	// first. Set by dragging, and meaningless on a card that is not pinned.
	// Zero on every card until something drags one, which leaves the pinned
	// set tied and falling back to the sort underneath.
	PinOrder int `json:"pin_order"`
	// SpawnedBy is the handle of the session that launched this card, `@human`
	// for the board's own launch dialog, and empty for a session nobody
	// launched. SpawnedByID is that session's card id when it was known at
	// launch. Written once, by `SetLineage`. See docs/agent-lineage-design.md.
	SpawnedBy   string `json:"spawned_by,omitempty"`
	SpawnedByID string `json:"spawned_by_id,omitempty"`
	// ReportedAt is the last time this card said something to its launcher: a
	// structured report, or a peer message to it. See
	// docs/a2a-reliability-design.md.
	ReportedAt *time.Time `json:"reported_at,omitempty"`
	// ReportSHA is the commit a `done` report named. ReportUnverified is set
	// when that commit is not in this card's worktree, and the board flags it.
	ReportSHA        string `json:"report_sha,omitempty"`
	ReportUnverified bool   `json:"report_unverified,omitempty"`
	// ToolHookSeenAt and StopHookSeenAt are when the two hooks that can carry
	// a queued message into the model were first heard from on this card. Nil
	// means a message queued here has no hook known to take it.
	ToolHookSeenAt *time.Time `json:"tool_hook_seen_at,omitempty"`
	StopHookSeenAt *time.Time `json:"stop_hook_seen_at,omitempty"`
	// PromptedAt is when this card was last given something to do: a prompt,
	// a typed message, or a queued one a hook carried in. Stamped wherever a
	// `prompted` event is written.
	PromptedAt *time.Time `json:"prompted_at,omitempty"`
	// Theme names the terminal palette this session uses. Held on the card so
	// it survives a restart and follows the session into another browser,
	// which is the point of coloring terminals: telling them apart at a
	// glance, permanently. Empty means the board picks from the project name.
	Theme string `json:"theme"`
	// Sound names the tone this card rings with. Held on the card for the same
	// reason as the theme: telling sessions apart without looking only works
	// if the answer is the same tomorrow and in another browser. Empty means
	// the board-wide default for whichever kind of alert fired.
	Sound string `json:"sound"`
	// Icon is the mark this card wears on a desktop notification, beside the
	// theme and the tone for the same reason: a notification arrives with the
	// operating system's chrome around it and one small image, and the same A
	// on all of them says only that atrium sent it.
	//
	// Whatever renders in one glyph. A letter, a digit, an emoji. Empty means
	// the atrium mark.
	Icon string `json:"icon"`
	// Recap is what the session said it did, in its own words, and RecapAt is
	// when it said so.
	//
	// The point of this is that a session which ended with an account of
	// itself and one that did not are different, and until there was somewhere
	// to put one the board could not tell them apart. A card in `done` with no
	// recap is either still worth writing up or was never worth starting, and
	// both of those are worth being able to see.
	//
	// Not a transcript. Two or three sentences, written once at the end, by
	// the only party that knows what happened. Bounded on the way in.
	Recap   string     `json:"recap,omitempty"`
	RecapAt *time.Time `json:"recap_at,omitempty"`
	// Ask is what this session needs RIGHT NOW, and AskAt is when it said so.
	//
	// Its own field rather than `Why`, which is where an ask used to land.
	// Those are two different questions that read identically once they share
	// a line: `Why` is what the card is FOR, written once by the operator and
	// read in a week, and an ask is a question outstanding this minute. An
	// ask writing over it lost the standing answer to "what was I even doing"
	// to a question that would be stale by lunchtime, and nothing could put it
	// back.
	//
	// Cleared when the ask is answered, so a card holding one means somebody
	// still owes it something.
	Ask   string     `json:"ask,omitempty"`
	AskAt *time.Time `json:"ask_at,omitempty"`
	// AskPeer is the session an ask was routed to, or empty when it is on the
	// board for a human.
	//
	// The difference decides who is on the hook. A card that is stopped
	// waiting on another session should not read as one waiting on you, and
	// with only the ask stored there was no way to draw them apart.
	AskPeer string `json:"ask_peer,omitempty"`
	// Note is what you want to say next, written down while the agent is still
	// working and not sent until you say.
	//
	// Deliberately not the message queue, which fires as soon as there is
	// anything in it. What this buys is ordering: three things thought of
	// during a long turn, sent as one instruction at the end.
	Note string `json:"note,omitempty"`
	// Priority is how much this matters, and it is the ONE judgement on this
	// board. Everything else is a fact: what a runner is doing, how long it has
	// waited, what repository it is in.
	//
	// `high`, empty for normal, or `low`. Three levels rather than a number,
	// because three never need a tie break and a number turns into something to
	// fiddle with. See migration 0036 for the rest of the argument.
	//
	// NOT set by an agent, ever. This is the operator's judgement about their
	// own attention, which is the same argument that keeps the status column
	// human. A source may SUGGEST one on an offered item; accepting the item is
	// what writes it here.
	Priority string `json:"priority,omitempty"`
	// PriorityAt is when that judgement was made, so the board can fade one
	// that has not been touched in a month. Read at display time and never
	// written back: nothing acts on priority, so there is no moment to expire
	// it in and no timer that would own doing so.
	PriorityAt *time.Time `json:"priority_at,omitempty"`
	// ArchivedAt is when this card left the board, or nil while it is on it.
	//
	// Off the board, still on the record. A dead card is swept so the finished
	// column does not fill up all day, and deleting it would take the only
	// account of what that session ran and what it was allowed to do. The
	// board asks what wants attention now; the history asks what has ever run
	// here. Archiving is what lets those be different questions.
	ArchivedAt *time.Time `json:"archived_at,omitempty"`
}

// Display resolves the observed-versus-overrides rule: an override wins when
// one exists, otherwise the observed value stands. Observed data never
// overwrites an override, which is what lets a hand-picked title survive an
// agent reconnect.
func (t *Task) Display(field, observed string) string {
	if v, ok := t.Overrides[field]; ok && v != "" {
		return v
	}
	return observed
}

// DisplayTitle is the title a client should render.
func (t *Task) DisplayTitle() string { return t.Display("title", t.Title) }

// DisplayRepo is the repository a client should render, in three tiers.
//
// An override first, because a human typed it. Then whatever the launcher
// recorded, because whoever made the worktree knew. Then, and only then, a
// guess read off the path.
//
// THE GUESS IS NOT STORED AND NOT OBSERVED DATA. It is computed on the way out
// and nothing writes it back, so a launcher that starts sending the real answer
// tomorrow wins immediately and a wrong guess costs one override to correct.
// `docs/architecture-v2.md` says atrium is not learning git, and this does not:
// it reads a directory name out of a string it already has, the same way the
// board's own default grouping rule has always done.
func (t *Task) DisplayRepo() string {
	if v, ok := t.Overrides["repo"]; ok && v != "" {
		return v
	}
	if strings.TrimSpace(t.Repo) != "" {
		return t.Repo
	}
	return InferRepo(t.Worktree)
}

// forgeDir matches the directory a checkout tree is kept under.
//
// Prefix rather than exact, so `github`, `github.com` and `github-enterprise`
// all answer. The shape below them is the one every forge uses and the one the
// board's `DEFAULT_GROUP_BY` already keys on: <forge>/<org>/<repo>.
var forgeDir = regexp.MustCompile(`(?i)^(github|gitlab|bitbucket|gitea|codeberg)`)

// InferRepo reads a repository name out of a worktree path.
//
// The case this exists for: a card launched without `--repo`, whose worktree is
// several directories BELOW the checkout. Without a repo to anchor on, the
// terminal strip fell back to the last three path segments, so
//
//	D:/worktrees/github/openziti/desktop-edge-win/more-debug-skill-updates/doc/troubleshooting/debug-skill
//
// drew as three nested headings called doc, troubleshooting and debug-skill,
// each holding one row. Three directories that mean nothing, arranged as though
// they meant something.
//
// Returns empty when the path does not have the shape. An empty answer leaves
// every caller exactly where it was, which is the point: this improves the
// cards it recognises and cannot make any other card worse.
func InferRepo(worktree string) string {
	segs := strings.FieldsFunc(
		strings.ReplaceAll(strings.TrimSpace(worktree), `\`, "/"),
		func(r rune) bool { return r == '/' })
	// The forge, then the org, then the repo. Two segments have to follow it,
	// or what was found is the tail of a path rather than the start of a tree.
	for i, s := range segs {
		if forgeDir.MatchString(s) && i+2 < len(segs) {
			return segs[i+2]
		}
	}
	return ""
}

// AutoOn reports whether auto mode is in force for this card right now.
//
// The flag and the deadline together, so no caller has to remember that the
// two exist. A flag that is on with a deadline that has passed is off, and it
// stays that way whether or not anything has got round to writing it down.
func (t *Task) AutoOn(at time.Time) bool {
	if !t.AutoApprove {
		return false
	}
	return t.AutoUntil == nil || at.Before(*t.AutoUntil)
}

// AutoExpired reports a card whose auto mode has run out but still says it is
// on. The permission chain uses it to turn the flag off when it notices, so
// the badge on the board stops claiming something that stopped being true.
func (t *Task) AutoExpired(at time.Time) bool {
	return t.AutoApprove && t.AutoUntil != nil && !at.Before(*t.AutoUntil)
}

// Observed reports a session atrium can see but cannot talk to: adopted from
// an external source, with no agent connected. These are watchable, and can be
// resumed or opened, but there is nothing to send a prompt to.
func (t *Task) Observed() bool { return t.ExternalID != "" && t.WireName == "" }

// Observed is what a runner reports about itself on registration. Every field
// here is knowable by the agent's own process, so none of it is ever worth a
// model turn to ask for.
type Observed struct {
	WireName string `json:"wire_name"`
	Worktree string `json:"worktree"`
	Repo     string `json:"repo"`
	Runner   string `json:"runner"`
	Hostname string `json:"hostname"`
	PID      int    `json:"pid"`
}

// Event is one entry in a task's history.
type Event struct {
	ID      string          `json:"id"`
	TaskID  string          `json:"task_id"`
	At      time.Time       `json:"at"`
	Kind    string          `json:"kind"`
	Payload json.RawMessage `json:"payload"`
}

// Permission is one pending or resolved permission request.
type Permission struct {
	ID          string     `json:"id"`
	TaskID      string     `json:"task_id"`
	Tool        string     `json:"tool"`
	Command     string     `json:"command"`
	RequestedAt time.Time  `json:"requested_at"`
	DecidedAt   *time.Time `json:"decided_at,omitempty"`
	Decision    string     `json:"decision,omitempty"`
	Reason      string     `json:"reason"`
	DedupKey    string     `json:"dedup_key"`
	// DecidedBy is "you" for a hand-made decision, or the pattern of the
	// standing rule that answered it.
	DecidedBy string `json:"decided_by"`
	// RuleCreated is the pattern of the rule this decision established, set
	// when the answer was "always" or "never" rather than a one-off.
	RuleCreated string `json:"rule_created,omitempty"`
	// Details is what is actually changing: the diff for an edit, the content
	// for a write. A path alone says which file, not what happens to it.
	Details string `json:"details,omitempty"`
}

// Store owns the database, and whether it has halted.
type Store struct {
	db *sql.DB

	mu        sync.RWMutex
	haltCause error
	// closed is set by Close. See ErrClosed.
	closed atomic.Bool

	// OnHalt is called once, from the goroutine that hit the failure. The
	// daemon uses it to close the agent-facing listener and stop supervised
	// runners. It must not call back into the store.
	OnHalt func(cause error)

	// hot serves Recent and takes every event synchronously, on the halt path.
	// cold are write-only durability sinks fed best-effort. See eventsink.go.
	// Both are wired once at Open from the event_sink setting; the default is
	// the db table alone and no cold sinks, which is byte-for-byte today.
	hot  EventSink
	cold []EventSink

	fresh bool

	// incrementalVacuum is whether this database is in incremental auto_vacuum
	// mode, read back once at Open. Only such a database keeps free pages this
	// store can hand back to disk, so IncrementalVacuum is a no-op otherwise.
	incrementalVacuum bool
}

// Fresh reports whether Open created the database rather than found one.
//
// Opening the wrong path looks identical to every card and every rule having
// vanished. `WORKTREE_ROOT` unset once sent the path to the home directory and
// a hundred and twenty five rules appeared to be gone. A caller is expected to
// say loudly that it made a new one, and where.
func (s *Store) Fresh() bool { return s.fresh }

// Open opens the database and applies migrations. A failure here is tier one:
// the caller is expected to refuse to start rather than continue degraded.
func Open(path string) (*Store, error) {
	// Checked before opening, since opening creates the file. See Store.Fresh.
	fresh := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fresh = true
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One writer at a time. WAL keeps readers from blocking behind it, and
	// busy_timeout absorbs most contention before it ever reaches our retry
	// loop.
	db.SetMaxOpenConns(1)
	// auto_vacuum only takes on a database with no tables yet, so it has to be
	// set on a FRESH file before migrations create the schema, and before WAL is
	// turned on. This is the half that gives freed space back to disk: pages a
	// prune or an event roll-off releases are reclaimed incrementally while the
	// room stays live, rather than sitting at the file's high water mark.
	//
	// An existing file is left exactly as it was. Switching an existing database
	// to incremental needs a full VACUUM, which needs exclusive access a live
	// room cannot give without taking its terminals down. It keeps whatever mode
	// it was created with, and IncrementalVacuum below is a no-op on it.
	if fresh {
		if _, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
			db.Close()
			return nil, fmt.Errorf("PRAGMA auto_vacuum = INCREMENTAL: %w", err)
		}
	}
	for _, pragma := range []string{
		"PRAGMA journal_mode = WAL",
		"PRAGMA busy_timeout = 5000",
		"PRAGMA foreign_keys = ON",
		"PRAGMA synchronous = NORMAL",
	} {
		if _, err := db.Exec(pragma); err != nil {
			db.Close()
			return nil, fmt.Errorf("%s: %w", pragma, err)
		}
	}
	// Read back the mode that is actually in force. A fresh file is now
	// incremental; an existing one is whatever it was made as. An existing file
	// not in incremental mode is left alone, and said quietly so it is not a
	// mystery that the file never shrinks.
	incremental, err := autoVacuumIncremental(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read auto_vacuum: %w", err)
	}
	if !fresh && !incremental {
		log.Printf("store: %s is not in incremental auto_vacuum mode; freed pages will not shrink the file", path)
	}
	s := &Store{db: db, fresh: fresh, incrementalVacuum: incremental}
	// The default hot sink is the event table, so a store is usable before the
	// setting is read. configureSinks below may add cold sinks; it never
	// replaces this with anything that fails Recent.
	s.hot = newDBSink(db)
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.SeedHarnesses(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed harnesses: %w", err)
	}
	if err := s.SeedCardActions(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed card actions: %w", err)
	}
	// Read the event_sink setting and attach any cold sinks. A bad or unknown
	// setting is logged and skipped rather than fatal: a misconfigured cold
	// trail must never keep the daemon from starting.
	s.configureSinks(path)
	return s, nil
}

// Close releases the database, after flushing and closing any cold sinks so a
// buffered file sink writes what it is holding before the process exits.
func (s *Store) Close() error {
	s.closed.Store(true)
	for _, c := range s.cold {
		if cl, ok := c.(io.Closer); ok {
			if err := cl.Close(); err != nil {
				log.Printf("event sink: close: %v", err)
			}
		}
	}
	return s.db.Close()
}

// Halted reports whether the store has halted, and why.
func (s *Store) Halted() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.haltCause != nil, s.haltCause
}

// halt stops the store once and notifies the daemon.
func (s *Store) halt(cause error) {
	s.mu.Lock()
	if s.haltCause != nil {
		s.mu.Unlock()
		return
	}
	s.haltCause = cause
	cb := s.OnHalt
	s.mu.Unlock()
	if cb != nil {
		cb(cause)
	}
}

// transient reports whether an error is mere contention. Contention is not a
// failure: it clears in milliseconds and must never reach a caller.
func transient(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED,
			sqlite3.SQLITE_BUSY_SNAPSHOT, sqlite3.SQLITE_LOCKED_SHAREDCACHE:
			return true
		}
	}
	// The driver does not always wrap, so fall back to the message.
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") || strings.Contains(msg, "database table is locked")
}

const (
	retryAttempts = 12
	retryBase     = 2 * time.Millisecond
)

// guard runs a database operation, retrying contention and halting on anything
// else. Every store method goes through it.
func (s *Store) guard(op func() error) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if halted, cause := s.Halted(); halted {
		return fmt.Errorf("%w: %v", ErrHalted, cause)
	}
	delay := retryBase
	var err error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		err = op()
		if err == nil {
			return nil
		}
		if errors.Is(err, sql.ErrNoRows) {
			return err
		}
		// Closed while this call was in flight. Late, not broken. See ErrClosed.
		if s.closed.Load() {
			return ErrClosed
		}
		if !transient(err) {
			s.halt(err)
			return fmt.Errorf("%w: %v", ErrHalted, err)
		}
		time.Sleep(delay)
		if delay < 500*time.Millisecond {
			delay *= 2
		}
	}
	// Contention that never cleared is no longer contention.
	s.halt(fmt.Errorf("contention did not clear after %d attempts: %w", retryAttempts, err))
	return fmt.Errorf("%w: %v", ErrHalted, err)
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// now is a variable so a test can move time forward.
//
// Everything time-dependent in here is a window measured against a stored
// timestamp, and the only honest way to test a two minute window is to be on
// the other side of it. Sleeping for two minutes is not a test anybody runs.
var now = func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func ts(t time.Time) string { return t.UTC().Format(TimeFormat) }

// tsOrEmpty formats an optional timestamp, keeping "never" as the empty string
// the schema stores rather than a zero time that would read as 1970.
func tsOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ts(*t)
}

func parseTS(s string) (time.Time, error) { return time.Parse(TimeFormat, s) }

// autoVacuumModeIncremental is the value PRAGMA auto_vacuum reports for a
// database in incremental mode. 0 is none, 1 is full, 2 is incremental.
const autoVacuumModeIncremental = 2

// autoVacuumIncremental reads back whether the open database is in incremental
// auto_vacuum mode.
func autoVacuumIncremental(db *sql.DB) (bool, error) {
	var mode int
	if err := db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		return false, err
	}
	return mode == autoVacuumModeIncremental, nil
}

// IncrementalVacuumPages bounds how many free pages one vacuum tick hands back.
// Small on purpose: the pragma takes the write lock while it runs, and this is
// meant to trim the file gradually rather than block a request with a long
// reclaim. At the default page size this is roughly a megabyte a tick.
const IncrementalVacuumPages = 256

// IncrementalVacuum hands a bounded batch of free pages back to disk.
//
// A no-op on a database not in incremental mode, and a no-op with no free pages
// even when it is, so it is safe to call on a timer against any store. Routed
// through guard like every other write, so contention retries and a hard
// failure halts.
func (s *Store) IncrementalVacuum() error {
	if !s.incrementalVacuum {
		return nil
	}
	return s.guard(func() error {
		_, err := s.db.Exec(fmt.Sprintf("PRAGMA incremental_vacuum(%d)", IncrementalVacuumPages))
		return err
	})
}

// IncrementalVacuumOn reports whether this database can hand freed pages back to
// disk. False for an existing database created before incremental mode, whose
// file stays at its high water mark.
func (s *Store) IncrementalVacuumOn() bool { return s.incrementalVacuum }
