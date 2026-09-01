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
//  3. Any other failure. The store wedges: it records the cause, refuses all
//     further work, and calls OnWedge so the daemon can close the agent-facing
//     listener. A closed listener reads as connection-refused to an agent,
//     which is the one failure the agent client already absorbs silently, so
//     the fleet parks instead of burning tokens.
package store

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
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

// ErrWedged is returned by every store call once the store has wedged.
var ErrWedged = errors.New("store is wedged")

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
	Branch     string `json:"branch,omitempty"`
	WindowName string `json:"window_name,omitempty"`
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

// Store owns the database and the wedge state.
type Store struct {
	db *sql.DB

	mu         sync.RWMutex
	wedgeCause error

	// OnWedge is called once, from the goroutine that trips the wedge. The
	// daemon uses it to close the agent-facing listener and stop supervised
	// runners. It must not call back into the store.
	OnWedge func(cause error)
}

// Open opens the database and applies migrations. A failure here is tier one:
// the caller is expected to refuse to start rather than continue degraded.
func Open(path string) (*Store, error) {
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
	s := &Store{db: db}
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

// Wedged reports whether the store has wedged, and why.
func (s *Store) Wedged() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.wedgeCause != nil, s.wedgeCause
}

// wedge trips the wedge exactly once and notifies the daemon.
func (s *Store) wedge(cause error) {
	s.mu.Lock()
	if s.wedgeCause != nil {
		s.mu.Unlock()
		return
	}
	s.wedgeCause = cause
	cb := s.OnWedge
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

// guard runs a database operation, retrying contention and wedging on anything
// else. Every store method goes through it.
func (s *Store) guard(op func() error) error {
	if wedged, cause := s.Wedged(); wedged {
		return fmt.Errorf("%w: %v", ErrWedged, cause)
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
			s.wedge(err)
			return fmt.Errorf("%w: %v", ErrWedged, err)
		}
		time.Sleep(delay)
		if delay < 500*time.Millisecond {
			delay *= 2
		}
	}
	// Contention that never cleared is no longer contention.
	s.wedge(fmt.Errorf("contention did not clear after %d attempts: %w", retryAttempts, err))
	return fmt.Errorf("%w: %v", ErrWedged, err)
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

func now() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func ts(t time.Time) string { return t.UTC().Format(TimeFormat) }

func parseTS(s string) (time.Time, error) { return time.Parse(TimeFormat, s) }
