//go:build integration

package home

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestAHomeOnARepoWithNoCommitsGetsItsOwnUnbornBranch(t *testing.T) {
	t.Parallel()
	clone := filepath.Join(t.TempDir(), "emptyrepo")
	if out, err := exec.Command("git", "init", "-q", "-b", "main", clone).CombinedOutput(); err != nil {
		t.Fatalf("git init: %v %s", err, out)
	}
	if !Unborn(clone) {
		t.Fatal("a fresh git init reads as having a commit")
	}
	dd := DirsOf(filepath.Join(t.TempDir(), "home"))
	branch, base, err := AddHomeWorktree(clone, dd, "", "", "first-card", nil)
	if err != nil {
		t.Fatalf("an empty repo refused the launch: %v", err)
	}
	if branch != "claude/first-card" || base != "" {
		t.Fatalf("branch %q base %q", branch, base)
	}
	out, err := exec.Command("git", "-C", dd.Work, "symbolic-ref", "--short", "HEAD").Output()
	if err != nil || strings.TrimSpace(string(out)) != "claude/first-card" {
		t.Fatalf("the worktree is on %q (%v)", out, err)
	}
}
