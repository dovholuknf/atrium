//go:build integration

package hubstore

import "testing"

// A STEP KEEPS THE TIME IT STARTED AND THE TIME IT ENDED while its state does not change, so a derivation that runs on
// every collect does not move them.
func TestAStepKeepsItsTimesAcrossRederivations(t *testing.T) {
	s := open(t)
	s.PutItem(onWork("a1", "sg4", "c1", "f-thing", 1))
	row := WorkStepRow{BacklogID: "f-thing", Step: "landed", State: StepActive, StartedAt: "2026-10-15T10:00:00Z"}
	if err := s.PutWorkStep(row); err != nil {
		t.Fatal(err)
	}
	row.State, row.StartedAt, row.EndedAt, row.SHA = StepDone, "2026-10-15T11:00:00Z", "2026-10-15T11:00:00Z", "abc"
	s.PutWorkStep(row)
	row.StartedAt, row.EndedAt = "2026-10-15T12:00:00Z", "2026-10-15T12:00:00Z"
	s.PutWorkStep(row)
	got, _ := s.WorkSteps("f-thing")
	if len(got) != 1 || got[0].StartedAt != "2026-10-15T10:00:00Z" || got[0].EndedAt != "2026-10-15T11:00:00Z" || got[0].Cards != "[]" {
		t.Fatalf("steps = %+v", got)
	}
	if err := s.SetWorkBranch("nope", "b"); err != ErrNoWork {
		t.Fatalf("a branch on no item: %v", err)
	}
	if err := s.PutWorkPaths("f-thing", []string{"a", "b"}); err != nil {
		t.Fatal(err)
	}
	s.PutWorkPaths("f-thing", nil)
	if p, _ := s.WorkPaths("f-thing"); len(p) != 2 {
		t.Fatalf("paths = %v", p)
	}
}
