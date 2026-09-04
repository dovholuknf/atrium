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
	"os"
	"strings"
	"sync"
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
)

// ErrHalted is returned by every store call once the store has halted.
var ErrHalted = errors.New("store is halted")

// Task is one card on the board.
type Task struct {
	ID             string            `json:"id"`
	Title          string            `json:"title"`
	Why            string            `json:"why"`
	Repo           string            `json:"repo"`
	Worktree       string            `json:"worktree"`
	Runner         string            `json:"runner"`
	Hostname       string            `json:"hostname"`
	PID            int               `json:"pid"`
	Status         string            `json:"status"`
	CreatedAt      time.Time         `json:"created_at"`
	LastActivityAt time.Time         `json:"last_activity_at"`
	WaitingSince   *time.Time        `json:"waiting_since,omitempty"`
	WireName       string            `json:"wire_name"`
	Overrides      map[string]string `json:"overrides"`
	Rank           float64           `json:"rank"`
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
	Branch     string `json:"branch,omitempty"`
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
	// Tags are what the operator calls this card, as opposed to what atrium
	// worked out from its path. Grouping already derives a project from the
	// worktree, which answers "what repo" and nothing else. A card is also a
	// support case, a tangent, a pull request, a lab, and none of that is in
	// the path.
	//
	// Free text on purpose. A fixed list would be atrium deciding what kinds
	// of work exist.
	Tags []string `json:"tags"`
	// Pinned keeps a card at the top of every list and always in the terminal
	// switcher. Some sessions are permanent fixtures and hunting for them in
	// activity order is the wrong shape.
	Pinned bool `json:"pinned"`
	// Theme names the terminal palette this session uses. Held on the card so
	// it survives a restart and follows the session into another browser,
	// which is the point of colouring terminals: telling them apart at a
	// glance, permanently. Empty means the board picks from the project name.
	Theme string `json:"theme"`
	// Sound names the tone this card rings with. Held on the card for the same
	// reason as the theme: telling sessions apart without looking only works
	// if the answer is the same tomorrow and in another browser. Empty means
	// the board-wide default for whichever kind of alert fired.
	Sound string `json:"sound"`
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

	// OnHalt is called once, from the goroutine that hit the failure. The
	// daemon uses it to close the agent-facing listener and stop supervised
	// runners. It must not call back into the store.
	OnHalt func(cause error)

	fresh bool
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
	s := &Store{db: db, fresh: fresh}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	if err := s.SeedHarnesses(); err != nil {
		db.Close()
		return nil, fmt.Errorf("seed harnesses: %w", err)
	}
	return s, nil
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

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
