//go:build integration

package gitexec

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// trapRepo is a repository whose own config runs a program on status, add and commit, logging to the second path.
func trapRepo(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	t.Setenv("GIT_CONFIG_GLOBAL", filepath.Join(t.TempDir(), "none"))
	root := t.TempDir()
	repo, hooks, ran := filepath.Join(root, "repo"), filepath.Join(root, "hooks"), filepath.ToSlash(filepath.Join(root, "ran"))
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
	for _, args := range [][]string{{"init", "-q", repo}, {"-C", repo, "config", "user.email", "t@x.invalid"},
		{"-C", repo, "config", "user.name", "t"}, {"-C", repo, "config", "core.fsmonitor", filepath.ToSlash(trap)},
		{"-C", repo, "config", "core.hooksPath", filepath.ToSlash(hooks)}, {"-C", repo, "config", "hook.t.event", "pre-commit"},
		{"-C", repo, "config", "--add", "hook.t.event", "post-index-change"},
		{"-C", repo, "config", "hook.t.command", "sh " + filepath.ToSlash(trap) + " config"}} {
		if out, err := exec.Command("git", args...).CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v %s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "f"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return repo, filepath.FromSlash(ran)
}

func TestCommandRunsNoProgramTheRepositoryNames(t *testing.T) {
	repo, ran := trapRepo(t)
	ctx := context.Background()
	for _, op := range []Op{Status{Untracked: "all"}, AddAll{}, Commit{Quiet: true, Message: "x"}} {
		if out, err := Run(ctx, Opts{Dir: repo, Combined: true}, op); err != nil {
			t.Fatalf("%T: %v %s", op, err, out)
		}
	}
	if b, err := os.ReadFile(ran); err == nil {
		t.Fatalf("the repository's config ran a program:\n%s", b)
	}
}

func TestIsolationRunsNoGitForAnUnchangedRepository(t *testing.T) {
	repo, _ := trapRepo(t)
	old := time.Now().Add(-time.Hour)
	for _, p := range []string{filepath.Join(repo, ".git"), filepath.Join(repo, ".git", "config")} {
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	has := func(kv []string, want string) bool {
		for _, s := range kv {
			if s == want {
				return true
			}
		}
		return false
	}
	if kv, err := Isolation(Opts{Dir: repo}); err != nil || !has(kv, "hook.t.enabled=false") {
		t.Fatalf("first read: %v %v", kv, err)
	}
	path := os.Getenv("PATH")
	t.Setenv("PATH", t.TempDir())
	if kv, err := Isolation(Opts{Dir: repo}); err != nil || !has(kv, "hook.t.enabled=false") {
		t.Fatalf("a second read of an unchanged repository ran git: %v %v", kv, err)
	}
	t.Setenv("PATH", path)
	if out, err := exec.Command("git", "-C", repo, "config", "hook.u.command", "x").CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	if kv, err := Isolation(Opts{Dir: repo}); err != nil || !has(kv, "hook.u.enabled=false") {
		t.Fatalf("a config change was not read: %v %v", kv, err)
	}
}
