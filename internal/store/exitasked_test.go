package store

import (
	"testing"
)

// AN ASKED EXIT IS A COLUMN, so no event setting can lose it: routing `notified`
// to the cold sink or compacting it away leaves the card down (r-new-review-ff747683).
func TestAnAskedExitSurvivesItsEventsGoing(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "w", Worktree: t.TempDir(), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if asked, _ := s.ExitAsked(task.ID); asked {
		t.Fatal("a new card reads as asked to exit")
	}
	if err := s.SetExitAsked(task.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(`DELETE FROM event WHERE task_id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	if asked, _ := s.ExitAsked(task.ID); !asked {
		t.Fatal("deleting the card's events lost the asked exit")
	}
	if err := s.ClearExitAsked(task.ID); err != nil {
		t.Fatal(err)
	}
	if asked, _ := s.ExitAsked(task.ID); asked {
		t.Fatal("a launch did not clear the asked exit")
	}
}

// THE MIGRATION CARRIES OVER what ff747683 wrote as events: the newest of an
// exit-asked and a launch decides.
func TestTheExitAskedBackfill(t *testing.T) {
	s := open(t)
	mk := func(name string) string {
		task, _, err := s.Register(Observed{WireName: name, Worktree: t.TempDir(), Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		return task.ID
	}
	asked, relaunched, never := mk("asked"), mk("relaunched"), mk("never")
	for _, id := range []string{asked, relaunched} {
		if err := s.AppendEvent(id, EventNotified, map[string]any{"by": ExitAskedBy}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.AppendEvent(relaunched, EventLaunched, map[string]any{"via": "test"}); err != nil {
		t.Fatal(err)
	}
	var backfill string
	for _, m := range migrations {
		if m.name == "0076_task_exit_asked" {
			backfill = m.stmts[1]
		}
	}
	if backfill == "" {
		t.Fatal("migration 0076 is not there")
	}
	if _, err := s.db.Exec(backfill); err != nil {
		t.Fatal(err)
	}
	for id, want := range map[string]bool{asked: true, relaunched: false, never: false} {
		if got, _ := s.ExitAsked(id); got != want {
			t.Errorf("%s reads asked=%v after the backfill, want %v", id, got, want)
		}
	}
}
