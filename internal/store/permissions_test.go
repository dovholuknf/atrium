package store

import "testing"

// Requests without a dedup key must not collide with each other. UNIQUE
// permits many NULLs but only one empty string, so an un-keyed request has to
// store NULL rather than ''.
func TestUnkeyedPermissionsDoNotCollide(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "agent-f", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	first, _, err := s.RecordPermission(task.ID, "Bash", "ls", "", "")
	if err != nil {
		t.Fatalf("first un-keyed request: %v", err)
	}
	second, _, err := s.RecordPermission(task.ID, "Bash", "pwd", "", "")
	if err != nil {
		t.Fatalf("second un-keyed request: %v", err)
	}
	if first.ID == second.ID {
		t.Fatal("two un-keyed requests collapsed into one row")
	}
	pending, err := s.PendingPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 2 {
		t.Fatalf("want 2 pending, got %d", len(pending))
	}
}

// The dedup key means "this agent's same request". Scoping it to the whole
// table meant two agents deriving keys the same way, from a hash of the
// command for instance, would collide and the second would be handed the
// first's answer.
func TestDedupKeyIsScopedToTheTask(t *testing.T) {
	s := open(t)
	a, _, err := s.Register(Observed{WireName: "agent-one", Worktree: "d:/one", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	b, _, err := s.Register(Observed{WireName: "agent-two", Worktree: "d:/two", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	first, _, err := s.RecordPermission(a.ID, "Bash", "go build ./...", "same-key", "")
	if err != nil {
		t.Fatalf("first agent: %v", err)
	}
	if _, err := s.DecidePermission(first.ID, "block", "not for you"); err != nil {
		t.Fatal(err)
	}

	// The other agent asks the same thing, with the same derived key.
	second, decided, err := s.RecordPermission(b.ID, "Bash", "go build ./...", "same-key", "")
	if err != nil {
		t.Fatalf("second agent collided with the first: %v", err)
	}
	if decided {
		t.Fatal("second agent was handed the first agent's decision")
	}
	if second.ID == first.ID {
		t.Fatal("both agents landed on one permission row")
	}
	if second.TaskID != b.ID {
		t.Fatalf("request recorded against the wrong task: %s", second.TaskID)
	}

	// Within one task the key still dedups, which is the point of it.
	replay, decided, err := s.RecordPermission(a.ID, "Bash", "go build ./...", "same-key", "")
	if err != nil {
		t.Fatal(err)
	}
	if !decided || replay.ID != first.ID {
		t.Fatal("a replay within the same task should return the original decision")
	}
}
