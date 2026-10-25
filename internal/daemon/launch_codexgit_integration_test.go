//go:build integration

package daemon

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitWT(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestOutsideGitDirNamesAWorktreesCloneOnly(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	clone := filepath.Join(root, "clone")
	gitWT(t, root, "init", "-q", "clone")
	gitWT(t, clone, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x")
	work := filepath.Join(root, "work")
	gitWT(t, clone, "worktree", "add", "-q", "-b", "card", work)

	if got := outsideGitDir(clone); got != "" {
		t.Fatalf("a plain clone added %q", got)
	}
	common := outsideGitDir(work)
	if !strings.EqualFold(filepath.Base(common), ".git") || !strings.EqualFold(filepath.Base(filepath.Dir(common)), "clone") {
		t.Fatalf("the worktree's git dir is %q, want the clone's .git", common)
	}
	if got := outsideGitDir(root); got != "" {
		t.Fatalf("a folder outside git added %q", got)
	}
}
