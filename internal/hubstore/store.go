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
// written back as fact. See docs/fabric/hub-decisions.md, 11 through 15.
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
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/dovholuknf/atrium/internal/base/dblock"

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

// ErrClosed is returned by every call after Close. A call that arrives once the hub has released the store is late,
// not a failure, and halting over it would report a clean stop as a broken database.
var ErrClosed = errors.New("the hub's store is closed")

// ErrNoSuchRoom is what a lookup for a room nobody added comes back as.
var ErrNoSuchRoom = errors.New("no room by that name")

// Store owns the hub's database, and whether it has halted.
type Store struct {
	db *sql.DB
	// clock, when set, replaces the wall clock for this store. A seam for tests.
	clock func() time.Time
	// afterPurgeCommit, when set, runs between a purge's commit and its file removal. A seam for tests.
	afterPurgeCommit func()
	// docsDir is where document bytes live, beside the database. See docs.go.
	docsDir string
	// docMu serialises a document write with a purge. See DocAdd.
	docMu sync.Mutex
	// filesDir is where file drop bytes live, beside the database. See files.go.
	filesDir string
	// filesMu serialises a drop write with a delete and the sweep. See FileAdd.
	filesMu sync.Mutex
	// gitPushClock is the clock the push log takes its row ids from. Nil is time.Now. A test sets it to model
	// a machine whose clock is behind.
	gitPushClock func() time.Time

	mu        sync.RWMutex
	haltCause error

	// closed is set by Close. See ErrClosed.
	closed atomic.Bool
	// inflight is the calls running an operation. Close waits for it to reach zero so no connection outlives it.
	inflight atomic.Int64

	// OnHalt is called once, from the goroutine that hit the failure. The hub
	// uses it to stop accepting rooms and to say on the board what broke. It
	// must not call back into the store.
	OnHalt func(cause error)

	fresh bool

	// incrementalVacuum is whether this database is in incremental auto_vacuum
	// mode, read back once at Open. Only such a database keeps free pages the
	// hub can hand back to disk, so VacuumLoop is a no-op otherwise.
	incrementalVacuum bool

	// lock is the hold on the file, nil for a shared open. Released by Close.
	lock *dblock.Lock
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
//
// It also takes the hold on the file (see dblock): a second Open of a file that is
// open elsewhere is refused with dblock.ErrHeld. A command that has to look at a
// live hub's database uses OpenShared.
func Open(path string) (*Store, error) { return openStore(path, true) }

// OpenShared is Open without the hold, for a short lived command run beside the
// hub that owns the file, such as adding a room. It never refuses on account of
// the hub, and so must not be used to run one.
func OpenShared(path string) (*Store, error) { return openStore(path, false) }

func openStore(path string, hold bool) (st *Store, err error) {
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
	var lock *dblock.Lock
	if hold {
		if lock, err = dblock.Acquire(path); err != nil {
			return nil, err
		}
		defer func() {
			if err != nil {
				lock.Release()
			}
		}()
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open %s: %w", path, err)
	}
	// One writer at a time, the same settings the room's store runs on. WAL
	// keeps readers off the writer's back and busy_timeout absorbs most
	// contention before it reaches the retry loop below.
	db.SetMaxOpenConns(1)
	// auto_vacuum only takes on a database with no tables yet, so it is set on a
	// FRESH file before migrations create the schema, and before WAL is turned
	// on. The hub's own store is small, but the room_card cache is rewritten
	// wholesale on every announce, which churns pages, and incremental mode is
	// what lets that freed space go back to disk while the hub stays up. An
	// existing file is left as it was: switching needs a full VACUUM.
	//
	// busy_timeout comes FIRST, ahead of every statement that takes a lock: two hubs
	// started at the same moment on one file would otherwise meet "database is
	// locked" here with no patience at all.
	if _, err := db.Exec("PRAGMA busy_timeout = 5000"); err != nil {
		db.Close()
		return nil, fmt.Errorf("PRAGMA busy_timeout = 5000: %w", err)
	}
	if fresh {
		_, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL")
		for try := 0; try < 20 && err != nil && raceLost(err); try++ {
			time.Sleep(25 * time.Millisecond)
			_, err = db.Exec("PRAGMA auto_vacuum = INCREMENTAL")
		}
		if err != nil {
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
		// Switching to WAL while another opener holds the file answers SQLITE_BUSY
		// without waiting, busy_timeout or not, so a lost race is asked again.
		_, err := db.Exec(pragma)
		for try := 0; try < 20 && err != nil && raceLost(err); try++ {
			time.Sleep(25 * time.Millisecond)
			_, err = db.Exec(pragma)
		}
		if err != nil {
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
	// Read back the mode in force. A fresh file is now incremental; an existing
	// one is whatever it was made as, and one not in incremental mode is left
	// alone and said quietly so a file that never shrinks is not a mystery.
	incremental, err := autoVacuumIncremental(db)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("read auto_vacuum: %w", err)
	}
	if !fresh && !incremental {
		log.Printf("[hub] %s is not in incremental auto_vacuum mode; freed pages will not shrink the file", path)
	}
	s := &Store{db: db, fresh: fresh, incrementalVacuum: incremental, lock: lock, docsDir: filepath.Join(filepath.Dir(path), "docs"),
		filesDir: filepath.Join(filepath.Dir(path), "files")}
	// Another hub opening this file at the same moment may create a table between
	// our check and our CREATE, which SQLite reports as "already exists" even with
	// IF NOT EXISTS. Every migration tolerates being there, so running again sees
	// the winner's work and carries on.
	err = s.migrate()
	for try := 0; try < 5 && err != nil && raceLost(err); try++ {
		time.Sleep(time.Duration(try+1) * 20 * time.Millisecond)
		err = s.migrate()
	}
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

// raceLost is whether a migration failed only because another opener of the same
// file got there first.
func raceLost(err error) bool {
	m := strings.ToLower(err.Error())
	return strings.Contains(m, "already exists") || strings.Contains(m, "database is locked") ||
		strings.Contains(m, "database schema has changed")
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

// Close releases the database once the calls already running have finished.
func (s *Store) Close() error {
	s.closed.Store(true)
	s.waitInflight()
	err := s.db.Close()
	s.lock.Release()
	return err
}

// closeWait bounds Close's wait for calls in flight. Each is bounded by busy_timeout, so this is never reached.
const closeWait = 10 * time.Second

// waitInflight lets the calls that were mid-operation when Close set the flag finish, so db.Close finds the one
// connection free and the file is released when Close returns.
func (s *Store) waitInflight() {
	deadline := time.Now().Add(closeWait)
	for s.inflight.Load() != 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if n := s.inflight.Load(); n != 0 {
		log.Printf("[hub] store: close: %d call(s) still running after %v", n, closeWait)
	}
}

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

// abandoned reports whether an error is a caller's context ending: cancelled, past its deadline, or the
// interrupt the driver raises for either. The database said nothing about itself.
func abandoned(err error) bool {
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var se *sqlite.Error
	return errors.As(err, &se) && se.Code() == sqlite3.SQLITE_INTERRUPT
}

const (
	retryAttempts = 12
	retryBase     = 2 * time.Millisecond
)

// guard runs one database operation, retrying contention and halting on
// anything else. Every method in this package goes through it.
func (s *Store) guard(op func() error) error {
	if s.closed.Load() {
		return ErrClosed
	}
	if halted, cause := s.Halted(); halted {
		return fmt.Errorf("%w: %v", ErrHalted, cause)
	}
	run := func() error {
		s.inflight.Add(1)
		defer s.inflight.Add(-1)
		if s.closed.Load() {
			return ErrClosed
		}
		return op()
	}
	delay := retryBase
	var err error
	for attempt := 0; attempt < retryAttempts; attempt++ {
		err = run()
		if err == nil {
			return nil
		}
		if errors.Is(err, ErrClosed) {
			return err
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
		// A CALLER THAT GAVE UP IS NOT A BROKEN DISK. A request whose client went away cancels its context, and
		// a query on that context answers "context canceled". Halting on it took every room down once (f-001).
		if abandoned(err) {
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

// IsRefusal reports whether an error is the store declining what the caller asked, which is the caller's to fix.
func IsRefusal(err error) bool { return deliberate(err) }

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

// wallNow is the clock a Store reads unless a test gave it its own. See `Store.now`.
func wallNow() time.Time { return time.Now().UTC().Truncate(time.Millisecond) }

// now is the store's clock. A test sets `clock` on its Store so it can move time forward rather than sleep through a
// window it is trying to measure.
func (s *Store) now() time.Time {
	if s.clock != nil {
		return s.clock()
	}
	return wallNow()
}

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

// SetClock replaces the store's clock, for a test in another package that has to move time past a window. Nil is the
// wall clock again.
func (s *Store) SetClock(f func() time.Time) { s.clock = f }
