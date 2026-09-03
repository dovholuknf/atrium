package daemon

import "testing"

// Named subagents, and the count that has to stay right even when the names
// do not arrive. Hooks are best effort, so every one of these is a way the two
// can fall out of step.

func TestSubagentsAreNamed(t *testing.T) {
	a := newActivityTracker()
	a.subagentStarted("t1", "ag-1", "explore")
	a.subagentStarted("t1", "ag-2", "review")

	got := a.get("t1")
	if got.Subagents != 2 {
		t.Fatalf("count is %d, wanted 2", got.Subagents)
	}
	if len(got.Running) != 2 {
		t.Fatalf("named %d, wanted 2", len(got.Running))
	}
	if got.Running[0].Type != "explore" || got.Running[1].Type != "review" {
		t.Fatalf("named %+v, wanted explore then review", got.Running)
	}
}

// Stopping drops the one that stopped, not the newest.
func TestStoppingRemovesTheRightOne(t *testing.T) {
	a := newActivityTracker()
	a.subagentStarted("t1", "ag-1", "explore")
	a.subagentStarted("t1", "ag-2", "review")
	a.subagentStopped("t1", "ag-1")

	got := a.get("t1")
	if got.Subagents != 1 {
		t.Fatalf("count is %d, wanted 1", got.Subagents)
	}
	if len(got.Running) != 1 || got.Running[0].Type != "review" {
		t.Fatalf("left %+v, wanted review", got.Running)
	}
}

// A runner that reports no id still moves the count. The number is the thing
// that must not lie, and "2 subagents: explore" is honest about knowing one of
// them.
func TestAnUnnamedSubagentStillCounts(t *testing.T) {
	a := newActivityTracker()
	a.subagentStarted("t1", "ag-1", "explore")
	a.subagentStarted("t1", "", "")

	got := a.get("t1")
	if got.Subagents != 2 {
		t.Fatalf("count is %d, wanted 2", got.Subagents)
	}
	if len(got.Running) != 1 {
		t.Fatalf("named %d, wanted 1", len(got.Running))
	}
}

// A stop for something never started still counts down. The stop is a fact
// even when the start that would have named it was lost, and refusing it
// leaves a session claiming a subagent that has finished.
func TestAnUnknownStopStillCountsDown(t *testing.T) {
	a := newActivityTracker()
	a.subagentStarted("t1", "ag-1", "explore")
	a.subagentStopped("t1", "ag-never-seen")

	got := a.get("t1")
	if got.Subagents != 0 {
		t.Fatalf("count is %d, wanted 0", got.Subagents)
	}
	// The named one is still listed, which is the honest answer: it was never
	// told that one stopped.
	if len(got.Running) != 1 {
		t.Fatalf("named %d, wanted the one that was never stopped", len(got.Running))
	}
}

// Hooks are retried, and two rows for one agent looks like real work.
func TestARepeatedStartIsOneSubagent(t *testing.T) {
	a := newActivityTracker()
	a.subagentStarted("t1", "ag-1", "explore")
	a.subagentStarted("t1", "ag-1", "explore")

	got := a.get("t1")
	if len(got.Running) != 1 {
		t.Fatalf("named %d, wanted 1", len(got.Running))
	}
}

// The count never goes below zero, whatever the hooks do.
func TestTheCountHasAFloor(t *testing.T) {
	a := newActivityTracker()
	a.subagentStopped("t1", "ag-1")
	a.subagentStopped("t1", "ag-2")

	if got := a.get("t1"); got.Subagents != 0 {
		t.Fatalf("count is %d, wanted 0", got.Subagents)
	}
}
