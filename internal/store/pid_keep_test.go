package store

import "testing"

// r-011: a report with no pid keeps the pid on file only when it comes from the
// same session. A different conversation means a different process, whose pid is
// not known yet, so the old one is dropped rather than trusted.
func TestAPidlessReportKeepsThePidOnlyForTheSameSession(t *testing.T) {
	s := openTestStore(t)
	task, _, err := s.Register(Observed{WireName: "pidkeep", Worktree: "/tmp/pidkeep", Runner: "claude", PID: 4242})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetResumeID(task.ID, "conv-a"); err != nil {
		t.Fatal(err)
	}
	pid := func() int {
		t.Helper()
		got, err := s.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		return got.PID
	}

	// Same conversation, no pid: kept.
	if _, _, err := s.Register(Observed{WireName: "pidkeep", Resume: "conv-a"}); err != nil {
		t.Fatal(err)
	}
	if got := pid(); got != 4242 {
		t.Fatalf("a pidless report from the same session changed the pid to %d", got)
	}

	// No conversation named, no pid: taken as the same session, kept.
	if _, _, err := s.Register(Observed{WireName: "pidkeep"}); err != nil {
		t.Fatal(err)
	}
	if got := pid(); got != 4242 {
		t.Fatalf("a pidless report naming no session changed the pid to %d", got)
	}

	// Through Observe too, which a launched card's session hook uses.
	if _, err := s.Observe(task.ID, Observed{Resume: "conv-a"}); err != nil {
		t.Fatal(err)
	}
	if got := pid(); got != 4242 {
		t.Fatalf("Observe with no pid from the same session changed the pid to %d", got)
	}

	// A different conversation, no pid: the old pid is dropped.
	if _, _, err := s.Register(Observed{WireName: "pidkeep", Resume: "conv-b"}); err != nil {
		t.Fatal(err)
	}
	if got := pid(); got != 0 {
		t.Fatalf("a pidless report from a different session kept the old pid %d", got)
	}

	// A nonzero pid always replaces what is there.
	if _, _, err := s.Register(Observed{WireName: "pidkeep", PID: 5151, Resume: "conv-b"}); err != nil {
		t.Fatal(err)
	}
	if got := pid(); got != 5151 {
		t.Fatalf("a report with a new pid left %d", got)
	}
}
