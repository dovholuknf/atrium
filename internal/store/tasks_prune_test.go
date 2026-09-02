package store

import (
	"testing"
	"time"
)

func mk(t *testing.T, s *Store, name, status string) string {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: name, Worktree: "/tmp/atrium-test", Runner: "claude"})
	if err != nil {
		t.Fatalf("register %s: %v", name, err)
	}
	if err := s.SetStatus(task.ID, status); err != nil {
		t.Fatalf("status %s: %v", name, err)
	}
	return task.ID
}

// Cards pile up because nothing removes them. Sweeping has to take the finished
// ones and leave everything else, above all the shelved ones: shelving is the
// one act that says "come back to this".
func TestPruneTakesFinishedAndSparesTheRest(t *testing.T) {
	s := open(t)
	live := mk(t, s, "live", StatusRunning)
	shelved := mk(t, s, "shelved", StatusShelved)
	mk(t, s, "done", StatusDone)
	mk(t, s, "dead", StatusDead)

	n, err := s.Prune(0)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 2 {
		t.Fatalf("swept %d cards, wanted the 2 finished ones", n)
	}
	for _, id := range []string{live, shelved} {
		if _, err := s.Get(id); err != nil {
			t.Fatalf("a card that should have survived is gone: %v", err)
		}
	}
}

// Asking for a shelved sweep has to come back empty rather than obeying.
func TestPruneRefusesShelved(t *testing.T) {
	s := open(t)
	id := mk(t, s, "shelved", StatusShelved)

	n, err := s.Prune(0, StatusShelved)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d shelved cards, wanted none", n)
	}
	if _, err := s.Get(id); err != nil {
		t.Fatalf("shelved card was deleted anyway: %v", err)
	}
}

// One column at a time, so clearing done does not silently take dead with it.
func TestPruneNarrowsToOneStatus(t *testing.T) {
	s := open(t)
	mk(t, s, "done", StatusDone)
	dead := mk(t, s, "dead", StatusDead)

	n, err := s.Prune(0, StatusDone)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 1 {
		t.Fatalf("swept %d, wanted only the done card", n)
	}
	if _, err := s.Get(dead); err != nil {
		t.Fatalf("clearing done also took dead: %v", err)
	}
}

// An age cutoff keeps something that just finished, so a sweep on a timer never
// deletes a card seconds after it went done.
func TestPruneHonoursAge(t *testing.T) {
	s := open(t)
	id := mk(t, s, "done", StatusDone)

	n, err := s.Prune(time.Hour)
	if err != nil {
		t.Fatalf("prune: %v", err)
	}
	if n != 0 {
		t.Fatalf("swept %d cards younger than the cutoff", n)
	}
	if _, err := s.Get(id); err != nil {
		t.Fatalf("a fresh card was swept: %v", err)
	}
}

// Resume is the whole answer to a runner dying with the daemon, so a blank must
// never overwrite an id that works.
func TestSetResumeIDIgnoresBlank(t *testing.T) {
	s := open(t)
	id := mk(t, s, "r", StatusRunning)

	if err := s.SetResumeID(id, "abc-123"); err != nil {
		t.Fatalf("set: %v", err)
	}
	if err := s.SetResumeID(id, "  "); err != nil {
		t.Fatalf("blank set: %v", err)
	}
	got, err := s.Get(id)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ResumeID != "abc-123" {
		t.Fatalf("resume id is %q, a blank wiped it", got.ResumeID)
	}
}
