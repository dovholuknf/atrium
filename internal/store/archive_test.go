package store

import (
	"testing"
	"time"
)

// Archiving takes a dead card off the board and keeps the row, so the history
// of what has ever run here survives a sweep.
func TestArchiveTakesDeadCardsOffTheBoard(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "over", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}

	n, err := s.Archive(0, StatusDead)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("archived %d, wanted 1", n)
	}

	live, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range live {
		if l.ID == task.ID {
			t.Fatal("an archived card is still on the board")
		}
	}
	// Kept, which is the whole difference from Prune.
	gone, err := s.ListArchived(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(gone) != 1 || gone[0].ID != task.ID {
		t.Fatalf("archived list has %d cards, wanted the one", len(gone))
	}
	if _, err := s.Get(task.ID); err != nil {
		t.Fatalf("the row itself went: %v", err)
	}
}

// A card that comes back to life comes back onto the board.
//
// This is the one that bit. A dead card is archived on a timer, and a dead
// card revives all the time: its session says something, or a fixture starts
// it again. Leaving the stamp on made the card invisible to every board query
// while it was plainly running, which reached the operator as a terminal that
// had started and was nowhere to be seen.
func TestRevivingACardUnarchivesIt(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "risen", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(0, StatusDead); err != nil {
		t.Fatal(err)
	}

	// Alive again.
	if err := s.SetStatus(task.ID, StatusNeedsInput); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != nil {
		t.Fatalf("a revived card is still stamped archived at %v", got.ArchivedAt)
	}
	live, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, l := range live {
		if l.ID == task.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("a revived card is not on the board")
	}
}

// Going from one over-and-done status to the other keeps the stamp. Nothing
// came back to life, so nothing should return to the board.
func TestMovingBetweenFinishedStatusesStaysArchived(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "filed", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(0, StatusDead); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDone); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("moving from dead to done put an archived card back on the board")
	}
}

// Shelving is never swept, because it says the work is coming back.
func TestArchiveNeverTakesShelved(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "putdown", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusShelved); err != nil {
		t.Fatal(err)
	}

	if _, err := s.Archive(0, StatusShelved, StatusDead, StatusDone); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != nil {
		t.Fatal("a shelved card was archived")
	}
}

// The age is respected, so a card that has just died is readable for a moment
// before it goes. A card that failed to start puts its reason on itself, and
// that reason is the one thing worth seeing.
func TestArchiveRespectsTheAge(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "recent", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}

	if n, err := s.Archive(time.Hour, StatusDead); err != nil {
		t.Fatal(err)
	} else if n != 0 {
		t.Fatalf("archived %d cards that were too recent", n)
	}
}
