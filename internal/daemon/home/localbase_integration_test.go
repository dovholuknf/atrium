//go:build integration

package home

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-C", dir, "-c", "commit.gpgsign=false", "-c", "user.name=t", "-c", "user.email=t@t"}, args...)
	out, err := exec.Command("git", full...).CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v %s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestALocalRepoWithNoOriginBranchesFromHeadAndClaudeMainStillWins(t *testing.T) {
	t.Parallel()
	clone := filepath.Join(t.TempDir(), "local")
	gitT(t, filepath.Dir(clone), "init", "-q", "-b", "claude/trunk", clone)
	gitT(t, clone, "commit", "-q", "--allow-empty", "-m", "one")
	head := gitT(t, clone, "rev-parse", "HEAD")

	dd := DirsOf(filepath.Join(t.TempDir(), "home"))
	branch, base, err := AddHomeWorktree(clone, dd, "", "", "a", nil)
	if err != nil {
		t.Fatalf("a repo with no origin and no claude/main was refused: %v", err)
	}
	if branch != "claude/a" || base != "claude/trunk" {
		t.Fatalf("branch %q base %q", branch, base)
	}
	if got := gitT(t, dd.Work, "rev-parse", "HEAD"); got != head {
		t.Fatalf("the worktree is at %s, not the repo's HEAD %s", got, head)
	}

	gitT(t, clone, "branch", "claude/main")
	gitT(t, clone, "commit", "-q", "--allow-empty", "-m", "two")
	dd2 := DirsOf(filepath.Join(t.TempDir(), "home"))
	_, base, err = AddHomeWorktree(clone, dd2, "", "", "b", nil)
	if err != nil || base != "claude/main" {
		t.Fatalf("base %q err %v, want claude/main", base, err)
	}
	if got := gitT(t, dd2.Work, "rev-parse", "HEAD"); got != head {
		t.Fatalf("the worktree is at %s, not claude/main %s", got, head)
	}
}
