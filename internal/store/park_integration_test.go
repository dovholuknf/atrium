//go:build integration

package store

import (
	"encoding/json"
	"testing"
	"time"
)

func parkCardFor(t *testing.T, s *Store) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: "p", Worktree: "/tmp/p", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func TestHumanTouchSurvivesRestart(t *testing.T) {
	path := t.TempDir() + "/atrium.db"
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task := parkCardFor(t, first)
	at := time.Now().UTC().Truncate(time.Second)
	if err := first.TouchHuman(task.ID, "typed", at); err != nil {
		t.Fatal(err)
	}
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	got, err := second.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.HumanAt == nil || !got.HumanAt.Equal(at) || got.HumanVia != "typed" {
		t.Fatalf("after a restart human_at %v via %q, want %v typed", got.HumanAt, got.HumanVia, at)
	}
	if got.ParkedAt != nil {
		t.Fatal("a card nobody parked is parked")
	}
}

func TestMigrationHumanAtParkedAtIsTolerant(t *testing.T) {
	path := t.TempDir() + "/atrium.db"
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	// Forget the migration so it runs again over columns that already exist.
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0071_human_at_parked_at'`); err != nil {
		t.Fatal(err)
	}
	s.Close()
	again, err := Open(path)
	if err != nil {
		t.Fatalf("the migration is not tolerant of already being there: %v", err)
	}
	again.Close()
}

func TestParkKeepsStatusAndWritesOneEvent(t *testing.T) {
	s, err := Open(t.TempDir() + "/atrium.db")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	task := parkCardFor(t, s)
	if err := s.SetStatus(task.ID, StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	// The wind-down files it dead, which is the hazard park has to undo.
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}
	before, _ := s.Events(task.ID, 100)

	ok, err := s.Park(task.ID, StatusNeedsInput, map[string]any{"by": "idle"})
	if err != nil || !ok {
		t.Fatalf("park: %v %v", ok, err)
	}
	got, _ := s.Get(task.ID)
	if got.Status != StatusNeedsInput || got.ParkedAt == nil {
		t.Fatalf("status %q parked_at %v", got.Status, got.ParkedAt)
	}
	after, _ := s.Events(task.ID, 100)
	if len(after) != len(before)+1 {
		t.Fatalf("park wrote %d events, want one", len(after)-len(before))
	}
	var payload map[string]any
	last := after[len(after)-1]
	if last.Kind != EventStatusChanged || json.Unmarshal(last.Payload, &payload) != nil ||
		payload["parked"] != true || payload["by"] != "idle" {
		t.Fatalf("event %s %s", last.Kind, last.Payload)
	}
	if again, _ := s.Park(task.ID, StatusNeedsInput, nil); again {
		t.Fatal("a parked card was parked twice")
	}

	was, err := s.Unpark(task.ID, "resume")
	if err != nil || !was {
		t.Fatalf("unpark: %v %v", was, err)
	}
	got, _ = s.Get(task.ID)
	if got.ParkedAt != nil {
		t.Fatal("still parked")
	}
	if again, _ := s.Unpark(task.ID, "resume"); again {
		t.Fatal("unparked twice")
	}
}
