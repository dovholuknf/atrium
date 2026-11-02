//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func hubWtGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	c := exec.Command("git", append([]string{"-C", dir}, args...)...)
	c.Env = append(os.Environ(), "GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t", "GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
	out, err := c.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func TestScopeHubRemoteIsWorktreeOnly(t *testing.T) {
	main := t.TempDir()
	hubWtGit(t, main, "init", "-q", "-b", "main")
	hubWtGit(t, main, "commit", "-q", "--allow-empty", "-m", "x")
	other := "http://127.0.0.1:7787/git/hub/github/o/r.git"
	hubWtGit(t, main, "remote", "add", "hub", other)
	wt := filepath.Join(t.TempDir(), "wt")
	hubWtGit(t, main, "worktree", "add", "-q", "-b", "w", wt)

	d := &Daemon{hk: *testHooks()}

	d.initServices()
	d.HubGit.Agent = "127.0.0.1:7777"
	d.scopeHubRemote(wt)

	if got := hubWtGit(t, wt, "config", "remote.hub.url"); got != "http://127.0.0.1:7777/git/hub/github/o/r.git" {
		t.Fatalf("worktree url = %s", got)
	}
	if got := hubWtGit(t, main, "config", "remote.hub.url"); got != other {
		t.Fatalf("main checkout url changed: %s", got)
	}
}

func TestScopeHubRemoteNoOps(t *testing.T) {
	d := &Daemon{hk: *testHooks()}
	d.initServices()
	d.HubGit.Agent = "127.0.0.1:7777"
	d.scopeHubRemote(t.TempDir()) // not a git dir

	r := t.TempDir()
	hubWtGit(t, r, "init", "-q")
	d.scopeHubRemote(r) // no hub remote
	hubWtGit(t, r, "remote", "add", "hub", "https://github.com/o/r.git")
	d.scopeHubRemote(r) // not a forwarder
	if got := hubWtGit(t, r, "config", "remote.hub.url"); got != "https://github.com/o/r.git" {
		t.Fatalf("url changed: %s", got)
	}
}

func TestScopeHubRemoteLeavesOneUrlPerWorktree(t *testing.T) {
	main := t.TempDir()
	hubWtGit(t, main, "init", "-q", "-b", "main")
	hubWtGit(t, main, "commit", "-q", "--allow-empty", "-m", "x")
	a := "http://127.0.0.1:7787/git/hub/github/o/r.git"
	b := "http://127.0.0.1:7777/git/hub/github/o/r.git"
	hubWtGit(t, main, "remote", "add", "hub", a)
	sib := filepath.Join(t.TempDir(), "sib")
	hubWtGit(t, main, "worktree", "add", "-q", "-b", "s", sib)
	wt := filepath.Join(t.TempDir(), "wt")
	hubWtGit(t, main, "worktree", "add", "-q", "-b", "w", wt)

	d := &Daemon{}

	d.initServices()
	d.HubGit.Agent = "127.0.0.1:7777"
	d.scopeHubRemote(wt)
	d.scopeHubRemote(wt) // idempotent

	for dir, want := range map[string]string{wt: b, main: a, sib: a} {
		if got := hubWtGit(t, dir, "config", "--get-all", "remote.hub.url"); got != want {
			t.Fatalf("%s urls = %q, want %q", dir, got, want)
		}
	}
}

func TestScopeHubRemoteRepairsDoubleUrl(t *testing.T) {
	main := t.TempDir()
	hubWtGit(t, main, "init", "-q", "-b", "main")
	hubWtGit(t, main, "commit", "-q", "--allow-empty", "-m", "x")
	a := "http://127.0.0.1:7787/git/hub/github/o/r.git"
	b := "http://127.0.0.1:7777/git/hub/github/o/r.git"
	hubWtGit(t, main, "remote", "add", "hub", a)
	wt := filepath.Join(t.TempDir(), "wt")
	hubWtGit(t, main, "worktree", "add", "-q", "-b", "w", wt)
	// the old behaviour: a second url in the worktree
	hubWtGit(t, main, "config", "extensions.worktreeConfig", "true")
	hubWtGit(t, wt, "config", "--worktree", "remote.hub.url", b)
	if n := len(strings.Fields(hubWtGit(t, wt, "config", "--get-all", "remote.hub.url"))); n != 2 {
		t.Fatalf("setup should double the url, got %d", n)
	}

	d := &Daemon{}

	d.initServices()
	d.HubGit.Agent = "127.0.0.1:7777"
	d.scopeHubRemote(wt)

	if got := hubWtGit(t, wt, "config", "--get-all", "remote.hub.url"); got != b {
		t.Fatalf("worktree urls = %q", got)
	}
	if got := hubWtGit(t, main, "config", "--get-all", "remote.hub.url"); got != a {
		t.Fatalf("main urls = %q", got)
	}
}

func TestScopeHubRemoteWorktreeAddedAfterTheMove(t *testing.T) {
	main := t.TempDir()
	hubWtGit(t, main, "init", "-q", "-b", "main")
	hubWtGit(t, main, "commit", "-q", "--allow-empty", "-m", "x")
	a := "http://127.0.0.1:7787/git/hub/github/o/r.git"
	b := "http://127.0.0.1:7777/git/hub/github/o/r.git"
	hubWtGit(t, main, "remote", "add", "hub", a)
	first := filepath.Join(t.TempDir(), "first")
	hubWtGit(t, main, "worktree", "add", "-q", "-b", "f", first)
	d := &Daemon{}
	d.initServices()
	d.HubGit.Agent = "127.0.0.1:7777"
	d.scopeHubRemote(first) // the move: .git/config loses the url

	late := filepath.Join(t.TempDir(), "late")
	hubWtGit(t, main, "worktree", "add", "-q", "-b", "l", late)
	// some git versions copy main's config.worktree into a new worktree: make it start with none either way
	hubWtGit(t, late, "config", "--worktree", "--unset-all", "remote.hub.url")
	d.scopeHubRemote(late)

	for dir, want := range map[string]string{late: b, first: b, main: a} {
		if got := hubWtGit(t, dir, "config", "--get-all", "remote.hub.url"); got != want {
			t.Fatalf("%s urls = %q, want %q", dir, got, want)
		}
	}
}
