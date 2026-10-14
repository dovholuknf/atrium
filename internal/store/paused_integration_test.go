//go:build integration

package store

import (
	"path/filepath"
	"strings"
	"testing"
)

// The status `shelved` was renamed `paused`. A database written before the rename holds the old word in rows and in the
// CHECK on task.status, and the migration has to take both, and take them again without harm.
func TestPausedMigrationRenamesRowsAndWidensTheCheck(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	path := filepath.Join(t.TempDir(), "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	live := mk(t, s, "live", StatusRunning)
	old := mk(t, s, "old", StatusDone)

	// Put the database back as an older build left it: the old CHECK, a `shelved` row, no record of 0101.
	var def string
	if err := s.db.QueryRow(`SELECT sql FROM sqlite_master WHERE name = 'task'`).Scan(&def); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA writable_schema = ON`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE sqlite_master SET sql = ? WHERE name = 'task'`,
		strings.Replace(def, "'shelved','paused'", "'shelved'", 1)); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`PRAGMA writable_schema = OFF`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path) // reopen so the schema is read afresh
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`UPDATE task SET status = 'shelved' WHERE id = ?`, old); err != nil {
		t.Fatalf("the old CHECK should take shelved: %v", err)
	}
	if _, err := s.db.Exec(`UPDATE task SET status = 'paused' WHERE id = ?`, live); err == nil {
		t.Fatal("the old CHECK should refuse paused, or this test proves nothing")
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0101_status_paused'`); err != nil {
		t.Fatal(err)
	}
	s.Close()

	for round := 1; round <= 2; round++ { // the second is a migration finding its work already done
		s, err = Open(path)
		if err != nil {
			t.Fatalf("round %d: %v", round, err)
		}
		got, err := s.Get(old)
		if err != nil || got.Status != StatusPaused {
			t.Fatalf("round %d: the shelved card should read paused, got %+v err %v", round, got, err)
		}
		if got, _ := s.Get(live); got.Status != StatusRunning {
			t.Fatalf("round %d: another status moved: %s", round, got.Status)
		}
		if err := s.SetStatus(live, StatusPaused); err != nil {
			t.Fatalf("round %d: the widened CHECK should take paused: %v", round, err)
		}
		s.SetStatus(live, StatusRunning)
		if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0101_status_paused'`); err != nil {
			t.Fatal(err)
		}
		s.Close()
	}
}

// `shelved` is still accepted wherever a status comes in, and means `paused`.
func TestShelvedIsAnAliasForPaused(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	s := open(t)
	id := mk(t, s, "a", StatusRunning)
	if err := s.SetStatus(id, "shelved"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(id); got.Status != StatusPaused {
		t.Fatalf("status = %s, want paused", got.Status)
	}
	for _, filter := range []string{"shelved", "paused"} {
		list, err := s.List(filter)
		if err != nil || len(list) != 1 || list[0].ID != id {
			t.Fatalf("List(%q) = %v, %v", filter, list, err)
		}
	}
}
