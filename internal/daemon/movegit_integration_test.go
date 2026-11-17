//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMoveWipCommitRunsNoProgramTheWorktreeNames(t *testing.T) {
	root := t.TempDir()
	wt, hooks, ran := filepath.Join(root, "wt"), filepath.Join(root, "hooks"), filepath.ToSlash(filepath.Join(root, "ran"))
	trap := filepath.Join(root, "trap.sh")
	body := "#!/bin/sh\necho \"$0 $*\" >> \"" + ran + "\"\n"
	for _, p := range []string{trap, filepath.Join(hooks, "pre-commit"), filepath.Join(hooks, "post-index-change")} {
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	for _, args := range [][]string{{"init", "-q", wt}, {"-C", wt, "config", "core.fsmonitor", filepath.ToSlash(trap)},
		{"-C", wt, "config", "core.hooksPath", filepath.ToSlash(hooks)}, {"-C", wt, "config", "hook.t.event", "pre-commit"},
		{"-C", wt, "config", "hook.t.command", "sh " + filepath.ToSlash(trap) + " config"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(wt, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, args := range [][]string{append([]string{"status", "--porcelain"}, moveGitArgs...),
		append([]string{"add", "-A"}, moveGitArgs...), {"commit", "-m", "WIP: move to b"}} {
		if out, err := moveGit(wt, args...); err != nil {
			t.Fatalf("%v: %v %s", args, err, out)
		}
	}
	if b, err := os.ReadFile(filepath.FromSlash(ran)); err == nil {
		t.Fatalf("the worktree's config ran a program:\n%s", b)
	}
}
