//go:build integration

package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func keyedCard(t *testing.T, s *Store, wire, key string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: wire, Worktree: "/w/" + wire, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetLaunchKey(task.ID, key); err != nil {
		t.Fatal(err)
	}
	return task
}

// A card is found by the key it was filed under, and only by that one.
func TestACardIsFoundByItsLaunchKey(t *testing.T) {
	t.Parallel()
	s := open(t)
	a := keyedCard(t, s, "key-a", "k-1")
	keyedCard(t, s, "key-b", "k-2")

	got, err := s.GetByLaunchKey("k-1")
	if err != nil || got.ID != a.ID {
		t.Fatalf("by key: %+v %v", got, err)
	}
	if _, err := s.GetByLaunchKey("k-none"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("an unknown key found a card: %v", err)
	}
	if _, err := s.GetByLaunchKey(""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("an empty key found a card: %v", err)
	}
}

// A card with no key is never found by an empty one.
func TestACardWithoutAKeyIsNotFoundByTheEmptyKey(t *testing.T) {
	t.Parallel()
	s := open(t)
	keyedCard(t, s, "no-key", "")
	if _, err := s.GetByLaunchKey(""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("empty key matched: %v", err)
	}
}

// Past the window the key is let go of, so an old card does not answer a new launch.
func TestALaunchKeyIsLetGoOfAfterItsWindow(t *testing.T) {
	t.Parallel()
	s := open(t)
	keyedCard(t, s, "old", "k-old")
	s.clock = func() time.Time { return wallNow().Add(LaunchKeyWindow + time.Minute) }
	if _, err := s.GetByLaunchKey("k-old"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a key past its window still answered: %v", err)
	}
}

// A store from before the column has 0106 as its last migration. Reopening it adds the column and the key works.
func TestTheLaunchKeyMigrationUpgradesAnExistingStore(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, q := range []string{
		`DROP INDEX task_launch_key`,
		`ALTER TABLE task DROP COLUMN launch_key`,
		`DELETE FROM schema_migration WHERE name = '0107_task_launch_key'`,
	} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	old, _, err := s.Register(Observed{WireName: "before", Worktree: "/w/before", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	up, err := Open(path)
	if err != nil {
		t.Fatalf("reopening a store from before launch keys: %v", err)
	}
	defer up.Close()
	if err := up.SetLaunchKey(old.ID, "k-up"); err != nil {
		t.Fatalf("the column is not there: %v", err)
	}
	got, err := up.GetByLaunchKey("k-up")
	if err != nil || got.ID != old.ID {
		t.Fatalf("by key after the upgrade: %+v %v", got, err)
	}
}
