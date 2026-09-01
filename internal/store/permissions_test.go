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
	first, _, err := s.RecordPermission(task.ID, "Bash", "ls", "")
	if err != nil {
		t.Fatalf("first un-keyed request: %v", err)
	}
	second, _, err := s.RecordPermission(task.ID, "Bash", "pwd", "")
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
