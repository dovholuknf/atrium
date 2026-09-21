package hubstore

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// A fresh hub store comes up in incremental auto_vacuum mode, the mode that can
// only be chosen before any table exists.
func TestFreshHubStoreIsIncremental(t *testing.T) {
	s := open(t)
	if !s.incrementalVacuum {
		t.Fatal("a fresh hub store did not come up in incremental auto_vacuum mode")
	}
	var mode int
	if err := s.db.QueryRow("PRAGMA auto_vacuum").Scan(&mode); err != nil {
		t.Fatal(err)
	}
	if mode != autoVacuumModeIncremental {
		t.Fatalf("PRAGMA auto_vacuum is %d, want %d (incremental)", mode, autoVacuumModeIncremental)
	}
}

// An existing hub store made before incremental mode is left as it was.
func TestExistingHubStoreLeftAlone(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy-hub.db")
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
		t.Fatalf("opening an existing legacy hub store failed: %v", err)
	}
	defer s.Close()
	if s.incrementalVacuum {
		t.Fatal("an existing legacy hub store was switched to incremental mode")
	}
}

// vacuumOnce runs without error on a fresh incremental store and does not halt.
func TestHubVacuumRuns(t *testing.T) {
	s := open(t)
	if !s.incrementalVacuum {
		t.Skip("store is not incremental; nothing to reclaim")
	}
	if err := s.vacuumOnce(); err != nil {
		t.Fatalf("incremental vacuum failed on a fresh hub store: %v", err)
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("hub store halted running incremental vacuum: %v", cause)
	}
}
