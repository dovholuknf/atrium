package store

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"
)

func countExits(t *testing.T, s *Store, id string) int {
	t.Helper()
	evs, err := s.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == EventExited {
			n++
		}
	}
	return n
}

// The migration applies to a fresh database, and again to one that already
// has every migration recorded but the new one, with a card in it.
func TestThePtyRunMigrationAppliesFreshAndToAnExistingDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "old.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	var seen string
	if err := s.db.QueryRow(`SELECT name FROM schema_migration WHERE name = '0077_pty_run'`).Scan(&seen); err != nil {
		t.Fatalf("a fresh database did not record the migration: %v", err)
	}
	card, _, err := s.Register(Observed{WireName: "old-card", Worktree: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	// The shape of a database from before this change.
	if _, err := s.db.Exec(`DROP TABLE pty_run`); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0077_pty_run'`); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatalf("an existing database did not migrate: %v", err)
	}
	defer s.Close()
	if _, err := s.Get(card.ID); err != nil {
		t.Fatalf("the card did not survive: %v", err)
	}
	if err := s.RecordRun(PtyRun{RunID: NewRunID(), TaskID: card.ID, Kind: RunKindRunner}); err != nil {
		t.Fatalf("the table is not usable: %v", err)
	}
	// And once more with the table already there and the name forgotten.
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0077_pty_run'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("the migration does not tolerate already being there: %v", err)
	}
}

func TestARunIsRecordedWithItsHostAndNotFiled(t *testing.T) {
	s := open(t)
	card, _, _ := s.Register(Observed{WireName: "c", Worktree: "/w"})
	id := NewRunID()
	if err := s.RecordRun(PtyRun{RunID: id, TaskID: card.ID, Kind: RunKindShell, Host: "h1"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Run(id)
	if err != nil {
		t.Fatal(err)
	}
	if got.TaskID != card.ID || got.Kind != RunKindShell || got.Host != "h1" || got.Filed || got.Started.IsZero() {
		t.Fatalf("row %+v", got)
	}
	if _, err := s.Run("nope"); err != sql.ErrNoRows {
		t.Fatalf("an unknown run: %v", err)
	}
}

func TestAnExitIsFiledOncePerRun(t *testing.T) {
	s := open(t)
	card, _, _ := s.Register(Observed{WireName: "c", Worktree: "/w"})
	id := NewRunID()
	if err := s.RecordRun(PtyRun{RunID: id, TaskID: card.ID, Kind: RunKindRunner}); err != nil {
		t.Fatal(err)
	}
	f := ExitFiling{TaskID: card.ID, RunID: id, Payload: map[string]any{"exit_code": 1}, Why: "failed to start: x"}
	for i, want := range []bool{true, false, false} {
		got, err := s.FileExit(f)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("filing %d returned %v, want %v", i, got, want)
		}
	}
	if n := countExits(t, s, card.ID); n != 1 {
		t.Fatalf("%d exit events, want 1", n)
	}
	if r, _ := s.Run(id); r == nil || !r.Filed {
		t.Fatalf("row not marked filed: %+v", r)
	}
	// A later start's exit is its own.
	id2 := NewRunID()
	if err := s.RecordRun(PtyRun{RunID: id2, TaskID: card.ID, Kind: RunKindRunner}); err != nil {
		t.Fatal(err)
	}
	f.RunID = id2
	if got, err := s.FileExit(f); err != nil || !got {
		t.Fatalf("a later run's exit: %v %v", got, err)
	}
	if n := countExits(t, s, card.ID); n != 2 {
		t.Fatalf("%d exit events, want 2", n)
	}
}

// A start whose row never landed is still filed once.
func TestAnUnrecordedRunIsStillFiledOnce(t *testing.T) {
	s := open(t)
	card, _, _ := s.Register(Observed{WireName: "c", Worktree: "/w"})
	f := ExitFiling{TaskID: card.ID, RunID: NewRunID(), Payload: map[string]any{"exit_code": 0}}
	first, _ := s.FileExit(f)
	second, _ := s.FileExit(f)
	if !first || second || countExits(t, s, card.ID) != 1 {
		t.Fatalf("first=%v second=%v", first, second)
	}
}

// A run id filed against the wrong card is not that card's exit.
func TestAnExitForAnotherCardsRunIsRefused(t *testing.T) {
	s := open(t)
	a, _, _ := s.Register(Observed{WireName: "a", Worktree: "/a"})
	b, _, _ := s.Register(Observed{WireName: "b", Worktree: "/b"})
	id := NewRunID()
	_ = s.RecordRun(PtyRun{RunID: id, TaskID: a.ID, Kind: RunKindRunner})
	got, err := s.FileExit(ExitFiling{TaskID: b.ID, RunID: id, Payload: map[string]any{}})
	if err != nil || got || countExits(t, s, b.ID) != 0 {
		t.Fatalf("got=%v err=%v", got, err)
	}
}

func TestAnEmptyRunIDFilesEveryTime(t *testing.T) {
	s := open(t)
	card, _, _ := s.Register(Observed{WireName: "c", Worktree: "/w"})
	f := ExitFiling{TaskID: card.ID, Payload: map[string]any{}}
	_, _ = s.FileExit(f)
	_, _ = s.FileExit(f)
	if n := countExits(t, s, card.ID); n != 2 {
		t.Fatalf("%d exit events, want 2", n)
	}
}

func TestPtyHostIsOffUnlessSetOn(t *testing.T) {
	s := open(t)
	if s.PtyHostOn() {
		t.Fatal("on by default")
	}
	_ = s.SetSetting(SettingPtyHost, "on")
	if !s.PtyHostOn() {
		t.Fatal("not on after being set")
	}
}

func TestPruningRunRowsKeepsUnfiledAndRecentOnes(t *testing.T) {
	s := open(t)
	card, _, err := s.Register(Observed{WireName: "prune-card", Worktree: "/w"})
	if err != nil {
		t.Fatal(err)
	}
	old := now().Add(-30 * 24 * time.Hour)
	oldFiled, oldLive, newFiled := NewRunID(), NewRunID(), NewRunID()
	for _, r := range []PtyRun{
		{RunID: oldFiled, TaskID: card.ID, Kind: RunKindRunner, Started: old},
		{RunID: oldLive, TaskID: card.ID, Kind: RunKindRunner, Started: old},
		{RunID: newFiled, TaskID: card.ID, Kind: RunKindRunner},
	} {
		if err := s.RecordRun(r); err != nil {
			t.Fatal(err)
		}
	}
	for _, id := range []string{oldFiled, newFiled} {
		if _, err := s.FileExit(ExitFiling{TaskID: card.ID, RunID: id}); err != nil {
			t.Fatal(err)
		}
	}
	n, err := s.PrunePtyRuns(PtyRunKeep)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d rows (%v), want the one old filed row", n, err)
	}
	if _, err := s.Run(oldFiled); err == nil {
		t.Fatal("the old filed row is still there")
	}
	for _, id := range []string{oldLive, newFiled} {
		if _, err := s.Run(id); err != nil {
			t.Fatalf("row %s was pruned: %v", id, err)
		}
	}
}
