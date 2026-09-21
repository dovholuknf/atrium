package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A fresh database has to come up in incremental auto_vacuum mode, because the
// mode can only be chosen before any table exists. Getting this wrong means the
// file never hands freed pages back for the life of that database.
func TestFreshDatabaseIsIncremental(t *testing.T) {
	s := open(t)
	if !s.IncrementalVacuumOn() {
		t.Fatal("a fresh database did not come up in incremental auto_vacuum mode")
	}
	var mode int
	if err := s.db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != autoVacuumModeIncremental {
		t.Fatalf("PRAGMA auto_vacuum is %d, want %d (incremental)", mode, autoVacuumModeIncremental)
	}
}

// An existing database made before incremental mode is left exactly as it was.
// Switching it would need a full VACUUM, which needs exclusive access a live
// room cannot give, so Open must detect and leave rather than force it.
func TestExistingLegacyDatabaseIsLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")

	// A file that exists and is NOT in incremental mode: a plain table on the
	// SQLite default of auto_vacuum = NONE. Open sees a non-fresh file and must
	// not change its mode.
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE legacy (a TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatalf("opening an existing legacy database failed: %v", err)
	}
	defer s.Close()

	if s.Fresh() {
		t.Fatal("an existing file was reported as fresh")
	}
	if s.IncrementalVacuumOn() {
		t.Fatal("an existing legacy database was switched to incremental mode")
	}
	var mode int
	if err := s.db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode == autoVacuumModeIncremental {
		t.Fatalf("legacy database is now in incremental mode; it should have been left as NONE")
	}
}

// IncrementalVacuum has to run without error, both on a fresh incremental
// database where it does real work and on a legacy one where it is a no-op. It
// must never halt the store either way.
func TestIncrementalVacuumRuns(t *testing.T) {
	s := open(t)
	// Make some free pages to reclaim: fill a table, then drop it. On an
	// incremental database the pages land on the free list for the vacuum.
	if _, err := s.db.Exec("CREATE TABLE junk (a TEXT)"); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 500; i++ {
		if _, err := s.db.Exec("INSERT INTO junk (a) VALUES (?)", "xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.db.Exec("DROP TABLE junk"); err != nil {
		t.Fatal(err)
	}
	if err := s.IncrementalVacuum(); err != nil {
		t.Fatalf("incremental vacuum failed on a fresh database: %v", err)
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("store halted running incremental vacuum: %v", cause)
	}
}

// On a database not in incremental mode the call must be a silent no-op, not an
// error and not a halt.
func TestIncrementalVacuumNoopOnLegacy(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec("CREATE TABLE legacy (a TEXT)"); err != nil {
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if err := s.IncrementalVacuum(); err != nil {
		t.Fatalf("incremental vacuum on a legacy database should be a no-op, got: %v", err)
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("store halted on a no-op vacuum: %v", cause)
	}
}
