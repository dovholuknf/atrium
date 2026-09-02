package store

import (
	"testing"
)

func reviewTask(t *testing.T, s *Store) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{
		WireName: "rev", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func decided(t *testing.T, s *Store, taskID, tool, cmd, decision, by string) {
	t.Helper()
	p, _, err := s.RecordPermission(taskID, tool, cmd, "", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.DecidePermissionBy(p.ID, decision, "", by); err != nil {
		t.Fatal(err)
	}
}

// The review exists to be read to the end. A session that ran one test command
// four hundred times would otherwise bury the single destructive call that also
// went through, so identical commands fold into one line with a count.
func TestReviewFoldsRepeatedCommands(t *testing.T) {
	s := open(t)
	task := reviewTask(t, s)

	for i := 0; i < 5; i++ {
		decided(t, s, task.ID, "Bash", "go test ./...", "approve", "auto")
	}
	decided(t, s, task.ID, "Bash", "rm -rf build", "approve", "auto")

	rev, err := s.ReviewTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Total != 6 {
		t.Fatalf("total is %d, wanted 6: the count has to stay honest even when folded", rev.Total)
	}
	if len(rev.Groups) != 1 {
		t.Fatalf("%d groups, wanted 1", len(rev.Groups))
	}
	g := rev.Groups[0]
	if len(g.Entries) != 2 {
		t.Fatalf("%d entries, wanted 2 folded lines", len(g.Entries))
	}
	var folded *ReviewEntry
	for _, e := range g.Entries {
		if e.Command == "go test ./..." {
			folded = e
		}
	}
	if folded == nil {
		t.Fatal("the repeated command is missing")
	}
	if folded.Repeats != 5 {
		t.Fatalf("repeats is %d, wanted 5", folded.Repeats)
	}
}

// Only what nobody saw counts as unattended. A rule is a decision made once on
// purpose, and counting every time it fires would bury the requests that were
// never considered at all.
func TestReviewCountsOnlyAutoAsUnattended(t *testing.T) {
	s := open(t)
	task := reviewTask(t, s)

	decided(t, s, task.ID, "Bash", "a", "approve", "auto")
	decided(t, s, task.ID, "Bash", "b", "approve", "ls *")
	decided(t, s, task.ID, "Bash", "c", "approve", DecidedBySelf)

	rev, err := s.ReviewTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Unattended != 1 {
		t.Fatalf("unattended is %d, wanted 1: only the auto decision was unseen", rev.Unattended)
	}
}

// A block under auto mode means the session hit a wall and worked around it,
// which is exactly what a review should surface.
func TestReviewCountsBlocks(t *testing.T) {
	s := open(t)
	task := reviewTask(t, s)

	decided(t, s, task.ID, "Bash", "taskkill /F /IM claude.exe", "block", "taskkill")
	decided(t, s, task.ID, "Bash", "go build ./...", "approve", "auto")

	rev, err := s.ReviewTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Blocked != 1 {
		t.Fatalf("blocked is %d, wanted 1", rev.Blocked)
	}
}

// The tool with the most decisions nobody saw is what a person should read
// first. A tool whose every call matched a rule is the least interesting.
func TestReviewPutsUnattendedToolsFirst(t *testing.T) {
	s := open(t)
	task := reviewTask(t, s)

	for i := 0; i < 10; i++ {
		decided(t, s, task.ID, "Read", "file-"+string(rune('a'+i)), "approve", "Read")
	}
	decided(t, s, task.ID, "Bash", "curl example.com", "approve", "auto")

	rev, err := s.ReviewTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Groups[0].Tool != "Bash" {
		t.Fatalf("first group is %s, wanted Bash: ten rule matches outranked one "+
			"decision nobody saw", rev.Groups[0].Tool)
	}
}

// Undecided requests are not part of a review. Something still waiting has not
// happened yet.
func TestReviewIgnoresPendingRequests(t *testing.T) {
	s := open(t)
	task := reviewTask(t, s)

	if _, _, err := s.RecordPermission(task.ID, "Bash", "still waiting", "", ""); err != nil {
		t.Fatal(err)
	}

	rev, err := s.ReviewTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if rev.Total != 0 {
		t.Fatalf("total is %d, wanted 0: a pending request is not something that happened", rev.Total)
	}
}

// A task nothing ever asked about has to come back empty rather than error.
func TestReviewOfAnEmptyTask(t *testing.T) {
	s := open(t)
	task := reviewTask(t, s)

	rev, err := s.ReviewTask(task.ID)
	if err != nil {
		t.Fatalf("reviewing a quiet task failed: %v", err)
	}
	if rev.Total != 0 || len(rev.Groups) != 0 {
		t.Fatalf("a quiet task produced %+v", rev)
	}
}
