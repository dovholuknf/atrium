//go:build integration

package cli

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorktreeNewListRm(t *testing.T) {
	dir := t.TempDir()
	main := filepath.Join(dir, "repo")
	run := func(args ...string) {
		t.Helper()
		if out, err := gitOut(main, args...); err != nil {
			t.Fatalf("%v: %s", err, out)
		}
	}
	if err := os.MkdirAll(main, 0o755); err != nil {
		t.Fatal(err)
	}
	run("init", "-b", "claude/main")
	run("-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "--allow-empty", "-m", "x")
	if err := os.WriteFile(filepath.Join(dir, "target.md"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "target.md"), filepath.Join(main, "CLAUDE.md")); err != nil {
		t.Skip("no symlinks")
	}
	t.Chdir(main)

	var out bytes.Buffer
	root := filepath.Join(dir, "wts")
	if err := worktreeNew(&out, "w1", "claude/main", root); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Readlink(filepath.Join(root, "w1", "CLAUDE.md")); err != nil {
		t.Fatalf("CLAUDE.md not linked: %v", err)
	}
	if err := worktreeNew(&out, "w1", "claude/main", root); err == nil {
		t.Fatal("second new should be refused")
	}
	out.Reset()
	if err := worktreeList(&out, "claude/main"); err != nil || !strings.Contains(out.String(), "claude/w1") {
		t.Fatalf("list: %v %q", err, out.String())
	}
	if err := worktreeRm(&out, "w1", "claude/main"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, "w1")); !os.IsNotExist(err) {
		t.Fatal("worktree still there")
	}
	if _, err := exec.Command("git", "-C", main, "rev-parse", "--verify", "claude/w1").Output(); err == nil {
		t.Fatal("merged branch not deleted")
	}
}
