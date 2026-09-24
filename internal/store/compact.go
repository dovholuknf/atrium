package store

import (
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// CompactOptions are the optional trims Compact applies to the copy before it
// rebuilds it. The zero value trims nothing: the copy holds every row the input
// did, only packed tight and switched to incremental auto_vacuum.
type CompactOptions struct {
	// WindowBytes applies the event hot window to every card on the copy, the
	// same roll-off event_window_bytes applies live. Zero or less leaves every
	// event in place.
	WindowBytes int64
	// DropKinds deletes every event of these kinds from the copy, the offline
	// half of event_cold_kinds. Kinds the store reads back from the table
	// (created, submitted) are refused.
	DropKinds []string
}

// CompactResult is what a compaction did, for the caller to print.
type CompactResult struct {
	InBytes, OutBytes int64
	// EventsBefore and EventsAfter count event rows on the copy before and after
	// the trims, so a caller can say how much history the copy no longer holds.
	EventsBefore, EventsAfter int64
}

// ErrDatabaseInUse is returned when another connection holds the input open.
var ErrDatabaseInUse = errors.New("database is open elsewhere; stop whatever holds it first")

// Compact writes a packed copy of the database at in to out, a path that must
// not exist yet. It never changes in's rows and never replaces in: swapping the
// copy into place is the operator's move, made with the room stopped.
//
// It refuses a database somebody else has open. The input is opened with an
// exclusive lock and no busy wait, so a running room (or anything else) holding
// it makes this fail at once rather than copy a file that is changing under it.
// The lock is held until the copy is written, so nothing can open the input
// part way through.
//
// The copy is made with VACUUM INTO, trimmed per opt, then switched to
// incremental auto_vacuum and rebuilt with a full VACUUM, which is the one
// rebuild an existing file needs to change mode. A room opened on the result
// hands freed pages back to disk from then on.
//
// If the input has a -wal file, opening it replays that log into the input,
// the same as any SQLite open would. No row changes.
func Compact(in, out string, opt CompactOptions) (*CompactResult, error) {
	for _, k := range opt.DropKinds {
		if dbPinnedKinds[k] {
			return nil, fmt.Errorf("cannot drop %q events: the store reads them back from the db", k)
		}
	}
	inAbs, err := filepath.Abs(in)
	if err != nil {
		return nil, err
	}
	outAbs, err := filepath.Abs(out)
	if err != nil {
		return nil, err
	}
	if strings.EqualFold(filepath.Clean(inAbs), filepath.Clean(outAbs)) {
		return nil, errors.New("--out must be a different file from --in")
	}
	if _, err := os.Stat(inAbs); err != nil {
		return nil, err
	}
	if _, err := os.Stat(outAbs); err == nil {
		return nil, fmt.Errorf("%s already exists; compact never overwrites", outAbs)
	}
	res := &CompactResult{InBytes: fileAndWALSize(inAbs)}

	if err := vacuumInto(inAbs, outAbs); err != nil {
		return nil, err
	}
	if err := trimAndRebuild(outAbs, opt, res); err != nil {
		os.Remove(outAbs)
		return nil, err
	}
	res.OutBytes = fileAndWALSize(outAbs)
	return res, nil
}

// vacuumInto copies in to out under an exclusive lock on in. locking_mode
// EXCLUSIVE keeps the lock after the probe transaction ends, so it spans the
// VACUUM INTO, which cannot itself run inside a transaction.
func vacuumInto(in, out string) error {
	src, err := sql.Open("sqlite", in)
	if err != nil {
		return fmt.Errorf("open %s: %w", in, err)
	}
	defer src.Close()
	src.SetMaxOpenConns(1)
	for _, stmt := range []string{
		"PRAGMA busy_timeout = 0",
		"PRAGMA locking_mode = EXCLUSIVE",
	} {
		if _, err := src.Exec(stmt); err != nil {
			return fmt.Errorf("%s: %w", stmt, err)
		}
	}
	if _, err := src.Exec("BEGIN EXCLUSIVE"); err != nil {
		return fmt.Errorf("%w (%s: %v)", ErrDatabaseInUse, in, err)
	}
	if _, err := src.Exec("COMMIT"); err != nil {
		return err
	}
	if _, err := src.Exec("VACUUM INTO ?", out); err != nil {
		return fmt.Errorf("VACUUM INTO %s: %w", out, err)
	}
	return nil
}

// trimAndRebuild applies the options to the copy, switches it to incremental
// auto_vacuum, and rebuilds it. The copy is private to this process, so a full
// VACUUM here costs nobody anything.
func trimAndRebuild(path string, opt CompactOptions, res *CompactResult) error {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return err
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if err := db.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&res.EventsBefore); err != nil {
		return err
	}
	if len(opt.DropKinds) > 0 {
		marks := strings.TrimSuffix(strings.Repeat("?,", len(opt.DropKinds)), ",")
		args := make([]any, len(opt.DropKinds))
		for i, k := range opt.DropKinds {
			args[i] = k
		}
		if _, err := db.Exec(`DELETE FROM event WHERE kind IN (`+marks+`)`, args...); err != nil {
			return fmt.Errorf("drop kinds: %w", err)
		}
	}
	if opt.WindowBytes > 0 {
		if err := windowEveryCard(db, opt.WindowBytes); err != nil {
			return fmt.Errorf("apply window: %w", err)
		}
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM event`).Scan(&res.EventsAfter); err != nil {
		return err
	}

	if _, err := db.Exec("PRAGMA auto_vacuum = INCREMENTAL"); err != nil {
		return err
	}
	if _, err := db.Exec("VACUUM"); err != nil {
		return fmt.Errorf("VACUUM: %w", err)
	}
	if on, err := autoVacuumIncremental(db); err != nil || !on {
		return fmt.Errorf("copy did not switch to incremental auto_vacuum (err %v)", err)
	}
	var check string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&check); err != nil {
		return err
	}
	if check != "ok" {
		return fmt.Errorf("quick_check on the copy: %s", check)
	}
	return nil
}

// windowEveryCard runs the live roll-off once per card, so the copy holds what
// a room with event_window_bytes set would have kept.
func windowEveryCard(db *sql.DB, windowBytes int64) error {
	rows, err := db.Query(`SELECT DISTINCT task_id FROM event`)
	if err != nil {
		return err
	}
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	sink := newDBSink(db)
	for _, id := range ids {
		if err := sink.rollOff(id, windowBytes); err != nil {
			return err
		}
	}
	return nil
}

func fileAndWALSize(path string) int64 {
	var total int64
	for _, p := range []string{path, path + "-wal"} {
		if fi, err := os.Stat(p); err == nil {
			total += fi.Size()
		}
	}
	return total
}
