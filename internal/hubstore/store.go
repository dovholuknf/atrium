// Package hubstore is the hub's own durable state: which rooms exist, what
// they are called, how they may connect, their join secrets, which of them are
// marked for deletion, and a cache of what each one last said.
//
// ── why a hub that holds nothing has a database ──────────
//
// The hub's founding rule is that it holds no WORK. No sessions, no pseudo
// terminals, no agent processes, and no authority over any of them. Stopping
// it costs nobody a session, and that is the only property that matters. This
// package does not break that rule: nothing here is work.
//
// What it does hold is the hub's own truth, which nothing else can answer.
// Whether a room called `sparta` has already been made, what its name is
// supposed to be, and whether somebody asked for it to go away are facts about
// the hub. A room cannot be asked, because the question is whether the room
// exists at all.
//
// ── the two kinds of thing in here, and the rule ─────────
//
// CONFIGURATION IS THE HUB'S TRUTH. Rooms, names, transports, secrets,
// deletion state. Authoritative, written by a person, and the reason this
// package exists.
//
// THE CACHE IS NEVER AUTHORITATIVE. `room_card` is what a room last said,
// nothing more. While a room is connected the cache is written and never read,
// and the room's own answer is the answer. Nothing read from it is ever
// written back as fact. See docs/decisions.md, 11 through 15.
//
// ── when it fails ───────────────────────────────────────
//
// It halts, exactly as `internal/store` does and for the same reason: running
// without durable state is worse than not running. A hub that keeps serving
// while it cannot remember which rooms exist is a hub that will mint a second
// room under a name it has forgotten, or fail to refuse a deletion it has no
// record of.
//
// That the work is safe on the rooms is not a reason to stay up. It is the
// reason the halt costs little.
package hubstore

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"modernc.org/sqlite"
	sqlite3 "modernc.org/sqlite/lib"
)

// TimeFormat is how every timestamp column is written. RFC3339 in UTC with
// milliseconds sorts lexicographically, which is what lets the same column work
// on SQLite and Postgres without a native timestamp type.
const TimeFormat = "2006-01-02T15:04:05.000Z"

// ErrHalted is returned by every call once the store has halted.
var ErrHalted = errors.New("the hub's store is halted")

// ErrNoSuchRoom is what a lookup for a room nobody added comes back as.
var ErrNoSuchRoom = errors.New("no room by that name")

// Store owns the hub's database, and whether it has halted.
type Store struct {
	db *sql.DB

	mu        sync.RWMutex
	haltCause error

	// OnHalt is called once, from the goroutine that hit the failure. The hub
	// uses it to stop accepting rooms and to say on the board what broke. It
	// must not call back into the store.
	OnHalt func(cause error)

	fresh bool
}

// Fresh reports whether Open made the database rather than found one.
//
// A hub pointed at the wrong path looks exactly like every room having
// vanished, and the rooms themselves would still be dialling in and being
// refused. The caller is expected to say loudly that it made a new one, and
// where.
func (s *Store) Fresh() bool { return s.fresh }

// Open opens the hub's database and applies migrations.
//
// A FAILURE HERE MEANS THE HUB DOES NOT START. Not a warning and not a degraded
// mode: see the package comment.
func Open(path string) (*Store, error) {
	// Checked before opening, because opening creates the file.
	fresh := false
	if _, err := os.Stat(path); os.IsNotExist(err) {
		fresh = true
	}

	// THE DIRECTORY FIRST, because SQLite will not make one.
	//
	// On a machine that has never run a hub there is nothing under the state
	// directory at all, and this is the first thing that touches it: the
	// certificate authority is set up later. Without this a brand new hub dies
	// at startup with a message about a file it was going to create anyway.
	//
	// 0700 for the same reason the key directory is: this holds join secrets,
	// hashed, and a cache of what people are working on.
	if dir := filepath.Dir(path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return nil, fmt.Errorf("could not make %s: %w", dir, err)
		}
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One writer at a time, the same settings the room's store runs on. WAL
	// keeps readers off the writer's back and busy_timeout absorbs most
	// contention before it reaches the retry loop below.
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
	// A DATABASE THAT IS NOT ONE. `sql.Open` is lazy and the pragmas above pass
	// on a file full of rubbish, so a truncated or corrupt store would only be
	// found at the first query, which is after the board is up and a room has
	// been told it may attach. Asked here instead, where the answer is still
	// "refuse to start".
	if err := integrity(db); err != nil {
		db.Close()
		return nil, err
	}
	s := &Store{db: db, fresh: fresh}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// integrity asks SQLite whether the file is intact.
//
// `quick_check` rather than `integrity_check`: it reads the same structural
// damage and does not walk every index on a store that will mostly be a few
// dozen rows. The answer is the single word `ok` when there is nothing wrong.
func integrity(db *sql.DB) error {
	rows, err := db.Query("PRAGMA quick_check(1)")
	if err != nil {
		return fmt.Errorf("this does not open as a database: %w", err)
	}
	defer rows.Close()
	var said []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return fmt.Errorf("checking the database: %w", err)
		}
		said = append(said, line)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("checking the database: %w", err)
	}
	if len(said) == 1 && strings.EqualFold(said[0], "ok") {
		return nil
	}
	return fmt.Errorf("the hub's database is damaged and it will not start on it: %s",
		strings.Join(said, "; "))
}

// Close releases the database.
func (s *Store) Close() error { return s.db.Close() }

// Halted reports whether the store has halted, and why.
func (s *Store) Halted() (bool, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.haltCause != nil, s.haltCause
}

// halt stops the store once and tells the hub.
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

// transient reports whether an error is mere contention, which clears in
// milliseconds and must never reach a caller as a failure.
func transient(err error) bool {
	var se *sqlite.Error
	if errors.As(err, &se) {
		switch se.Code() {
		case sqlite3.SQLITE_BUSY, sqlite3.SQLITE_LOCKED,
			sqlite3.SQLITE_BUSY_SNAPSHOT, sqlite3.SQLITE_LOCKED_SHAREDCACHE:
			return true
		}
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "database is locked") ||
		strings.Contains(msg, "database table is locked")
}

const (
	retryAttempts = 12
	retryBase     = 2 * time.Millisecond
)

// guard runs one database operation, retrying contention and halting on
// anything else. Every method in this package goes through it.
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
		// ERRORS THAT ARE ANSWERS, not failures. A lookup that found nothing, a
		// name already taken, a join string already spent: all of them are the
		// database working exactly as asked, and halting on any of them would
		// mean a typo or a stale paste took the hub down.
		//
		// Told apart by where they came from rather than by matching messages.
		// Anything this package raises deliberately is wrapped by `refuse`;
		// everything else arrived from the driver and is a real failure.
		if errors.Is(err, sql.ErrNoRows) || isConstraint(err) || deliberate(err) {
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

// refusal marks an error this package raised on purpose from inside `guard`.
//
// WITHOUT IT, EVERY REFUSAL IS A HALT. `guard` cannot see the difference
// between "this join string was already spent", which it should hand back, and
// "the disk has gone", which has to stop the hub, because both arrive as an
// error from the same closure. Wrapping the first kind is what makes the
// distinction structural rather than a list of messages to keep matching.
type refusal struct{ error }

func (r refusal) Unwrap() error { return r.error }

// refuse wraps an answer so `guard` hands it back instead of halting.
func refuse(err error) error {
	if err == nil {
		return nil
	}
	return refusal{err}
}

func deliberate(err error) bool {
	var r refusal
	return errors.As(err, &r)
}

// isConstraint reports whether a failure is the schema refusing something the
// caller asked for, rather than the database being broken.
func isConstraint(err error) bool {
	if err == nil {
		return false
	}
	var se *sqlite.Error
	if errors.As(err, &se) {
		// The extended codes all share the primary one in their low byte.
		if se.Code()&0xff == sqlite3.SQLITE_CONSTRAINT {
			return true
		}
	}
	return strings.Contains(strings.ToLower(err.Error()), "constraint failed")
}

// tx runs several statements as one.
//
// WHOLESALE REPLACEMENT NEEDS THIS. A room coming back announces its entire
// state and the old rows are deleted before the new ones land, so a failure in
// between would leave a room with no cards at all and no way to tell that from
// a room that genuinely has none.
func (s *Store) tx(fn func(*sql.Tx) error) error {
	return s.guard(func() error {
		t, err := s.db.Begin()
		if err != nil {
			return err
		}
		if err := fn(t); err != nil {
			_ = t.Rollback()
			return err
		}
		return t.Commit()
	})
}

func newID() string { return uuid.Must(uuid.NewV7()).String() }

// now is a variable so a test can move time forward rather than sleep through
// a window it is trying to measure.
var now = func() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

func ts(t time.Time) string { return t.UTC().Format(TimeFormat) }

// tsOrEmpty keeps "never" as the empty string the schema stores, rather than a
// zero time that reads as 1970 on a board.
func tsOrEmpty(t *time.Time) string {
	if t == nil {
		return ""
	}
	return ts(*t)
}

// parseOrNil reads a timestamp column that is allowed to be empty.
func parseOrNil(s string) *time.Time {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	t, err := time.Parse(TimeFormat, s)
	if err != nil {
		return nil
	}
	return &t
}

// fold is how a room name is compared.
//
// ASCII rather than strings.ToLower, so a name folds identically on every
// machine regardless of locale. A Turkish dotless i is a real thing and a room
// name is not prose. This is `keyOf` in internal/link, and the two have to
// agree: the hub decides a name is taken using this, and the link layer routes
// to it using that.
func fold(s string) string {
	out := []byte(strings.TrimSpace(s))
	for i, c := range out {
		if c >= 'A' && c <= 'Z' {
			out[i] = c + 32
		}
	}
	return string(out)
}
