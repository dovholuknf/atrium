package gitsync

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
)

func testGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// a checkout with one commit, and a worktree of it
func checkoutAndWorktree(t *testing.T) (checkout, worktree string) {
	t.Helper()
	root := t.TempDir()
	checkout = filepath.Join(root, "checkout")
	worktree = filepath.Join(root, "worktree")
	testGit(t, root, "init", "-q", checkout)
	testGit(t, checkout, "commit", "-q", "--allow-empty", "-m", "first")
	testGit(t, checkout, "worktree", "add", "-q", "-b", "claude/x", worktree)
	return checkout, worktree
}

func TestSharesGitDirIsTrueForAWorktreeOfTheCheckoutAndFalseForAnotherRepo(t *testing.T) {
	checkout, worktree := checkoutAndWorktree(t)
	ctx := context.Background()
	if !SharesGitDir(ctx, Default, worktree, checkout) {
		t.Fatal("a worktree and its checkout were not seen as one repository")
	}
	other := filepath.Join(t.TempDir(), "other")
	testGit(t, filepath.Dir(other), "init", "-q", other)
	if SharesGitDir(ctx, Default, worktree, other) {
		t.Fatal("two repositories were seen as one")
	}
	if SharesGitDir(ctx, Default, worktree, filepath.Join(t.TempDir(), "missing")) {
		t.Fatal("a folder that is not there shares a git dir")
	}
}

func TestPointsAtOtherForwarderIsTrueOnlyForAnotherLoopbackForwarder(t *testing.T) {
	checkout, worktree := checkoutAndWorktree(t)
	ctx := context.Background()
	ours := "http://127.0.0.1:7777/git/"
	if PointsAtOtherForwarder(ctx, Default, worktree, ours) {
		t.Fatal("no hub remote at all")
	}
	testGit(t, checkout, "remote", "add", "hub", "http://127.0.0.1:8888/git/hub/github/o/r.git")
	if !PointsAtOtherForwarder(ctx, Default, worktree, ours) {
		t.Fatal("another room's forwarder was not seen")
	}
	if PointsAtOtherForwarder(ctx, Default, worktree, "http://127.0.0.1:8888/git/") {
		t.Fatal("our own forwarder was called another's")
	}
	testGit(t, checkout, "remote", "set-url", "hub", "https://example.com/git/hub/github/o/r.git")
	if PointsAtOtherForwarder(ctx, Default, worktree, ours) {
		t.Fatal("a remote server was called a forwarder on this machine")
	}
}
