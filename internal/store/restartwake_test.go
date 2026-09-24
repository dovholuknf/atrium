package store

import (
	"database/sql"
	"errors"
	"path/filepath"
	"testing"
	"time"
)

// The wake is about a restart, so it has to outlive the process that took it.
func TestARestartWakeSurvivesReopeningTheStore(t *testing.T) {
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "atrium.db"))
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	task, _, err := s.Register(Observed{WireName: "orchestrator", Worktree: "d:/git/atrium", Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetRestartWake(task.ID, "we up", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	ws, err := s.RestartWakes()
	if err != nil {
		t.Fatal(err)
	}
	if len(ws) != 1 || ws[0].TaskID != task.ID || ws[0].Text != "we up" || ws[0].By != "orchestrator" {
		t.Fatalf("the wake did not survive the reopen: %+v", ws)
	}
}

// One per card, and the newer one wins.
func TestANewerRestartWakeReplacesTheOlder(t *testing.T) {
	s := openTestStore(t)
	task, _, err := s.Register(Observed{WireName: "orchestrator", Worktree: "d:/git/atrium", Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, old, err := s.SetRestartWake(task.ID, "first", "a"); err != nil || old != nil {
		t.Fatalf("first wake: old %+v, err %v", old, err)
	}
	w, old, err := s.SetRestartWake(task.ID, "second", "b")
	if err != nil {
		t.Fatal(err)
	}
	if old == nil || old.Text != "first" {
		t.Fatalf("the replaced wake was not reported: %+v", old)
	}
	ws, _ := s.RestartWakes()
	if len(ws) != 1 || ws[0].Text != "second" {
		t.Fatalf("expected one wake saying second: %+v", ws)
	}
	// A take for the replaced wake leaves the newer one alone.
	if took, err := s.TakeRestartWake(task.ID, w.QueuedAt.Add(-time.Hour)); err != nil || took {
		t.Fatalf("a stale take removed the newer wake: took %v, err %v", took, err)
	}
	if took, err := s.TakeRestartWake(task.ID, w.QueuedAt); err != nil || !took {
		t.Fatalf("the take did not remove the wake: took %v, err %v", took, err)
	}
	if ws, _ := s.RestartWakes(); len(ws) != 0 {
		t.Fatalf("a taken wake is still there: %+v", ws)
	}
}

// A wake for a card that does not exist is refused without halting the store,
// and one with no text is refused before it is written.
func TestARestartWakeIsRefusedForNoCardOrNoText(t *testing.T) {
	s := openTestStore(t)
	if _, _, err := s.SetRestartWake("no-such-card", "we up", ""); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("a missing card answered %v", err)
	}
	if halted, cause := s.Halted(); halted {
		t.Fatalf("a missing card halted the store: %v", cause)
	}
	task, _, err := s.Register(Observed{WireName: "x", Worktree: "d:/x", Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.SetRestartWake(task.ID, "   ", ""); !errors.Is(err, ErrWakeText) {
		t.Fatalf("an empty wake answered %v", err)
	}
}
