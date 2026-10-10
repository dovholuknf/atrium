//go:build integration

package store

import (
	"path/filepath"
	"testing"
	"time"
)

func TestAMarkIsDueOnlyOnceItsTimeHasCome(t *testing.T) {
	s := open(t)
	_, worker, _ := ledgerPair(t, s)
	at := time.Now().Add(30 * time.Minute)
	if ok, err := s.MarkMerged(worker.ID, "claude/main", "abcdef0123", "claude/w1", at); err != nil || !ok {
		t.Fatalf("mark: ok=%v err=%v", ok, err)
	}
	if due, _ := s.DueCulls(time.Now()); len(due) != 0 {
		t.Fatalf("%d due before the time", len(due))
	}
	if due, _ := s.DueCulls(at.Add(time.Second)); len(due) != 1 || due[0].TaskID != worker.ID {
		t.Fatalf("due after the time = %+v", due)
	}
	v, err := s.MergedViewFor(worker.ID)
	if err != nil || v == nil || v.Into != "claude/main" || v.CullAt == nil || v.CullSeconds <= 0 {
		t.Fatalf("view = %+v err=%v", v, err)
	}
}

// A second merge must not push the deadline out from under whoever watches the chip.
func TestASecondMergeDoesNotMoveTheDeadline(t *testing.T) {
	s := open(t)
	_, worker, _ := ledgerPair(t, s)
	first := time.Now().Add(10 * time.Minute)
	if ok, _ := s.MarkMerged(worker.ID, "claude/main", "a", "b", first); !ok {
		t.Fatal("not marked")
	}
	if ok, _ := s.MarkMerged(worker.ID, "claude/main", "a", "b", first.Add(time.Hour)); ok {
		t.Fatal("a second mark replaced the first")
	}
}

// A new turn clears the mark, and the next merge marks it again.
func TestANewTurnClearsTheMark(t *testing.T) {
	s := open(t)
	_, worker, _ := ledgerPair(t, s)
	if err := s.SetStatus(worker.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	if ok, _ := s.MarkMerged(worker.ID, "claude/main", "a", "b", time.Now().Add(time.Hour)); !ok {
		t.Fatal("not marked")
	}
	if err := s.SetStatus(worker.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if w := mustItem(t, s, worker.ID); w.CullAt != nil || w.MergedAt != nil {
		t.Fatalf("the mark survived a new turn: %+v", w)
	}
	if ok, _ := s.MarkMerged(worker.ID, "claude/main", "a", "b", time.Now().Add(time.Hour)); !ok {
		t.Fatal("the next merge could not mark it again")
	}
}

// A hold drops the mark, is not marked over, is never due, and survives a
// restart and a new turn.
func TestAHoldKeepsACardAcrossARestartAndANewTurn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	_, worker, _ := ledgerPair(t, s)
	if ok, _ := s.MarkMerged(worker.ID, "claude/main", "a", "b", time.Now().Add(time.Hour)); !ok {
		t.Fatal("not marked")
	}
	if err := s.HoldCull(worker.ID, "clint"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	w := mustItem(t, s, worker.ID)
	if w.HeldBy != "clint" || w.CullAt != nil {
		t.Fatalf("after a restart: %+v", w)
	}
	if err := s.SetStatus(worker.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	if w := mustItem(t, s, worker.ID); w.HeldBy != "clint" {
		t.Fatalf("a new turn dropped the hold: %+v", w)
	}
	if ok, _ := s.MarkMerged(worker.ID, "claude/main", "a", "b", time.Now()); ok {
		t.Fatal("a held card was marked")
	}
	if due, _ := s.DueCulls(time.Now().Add(time.Hour)); len(due) != 0 {
		t.Fatalf("a held card is due: %+v", due)
	}
	v, _ := s.MergedViewFor(worker.ID)
	if v == nil || v.HeldBy != "clint" {
		t.Fatalf("the view does not say it is held: %+v", v)
	}
}

func TestAcceptMergedSetsAcceptedOnce(t *testing.T) {
	s := open(t)
	_, worker, _ := ledgerPair(t, s)
	if err := s.AcceptMerged(worker.ID); err != nil {
		t.Fatal(err)
	}
	if w := mustItem(t, s, worker.ID); w.State != WorkAccepted {
		t.Fatalf("state = %s, want accepted", w.State)
	}
	if err := s.AcceptMerged(worker.ID); err != nil {
		t.Fatal(err)
	}
}
