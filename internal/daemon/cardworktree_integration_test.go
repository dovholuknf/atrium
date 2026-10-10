//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A runner launched fresh has no resume spec, and still reports the directory
// it was started in, whatever the card says.
func TestRunnerDirIsTheLaunchDirectoryOnAFreshLaunch(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "fresh", Worktree: "/somewhere/else", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	launch := filepath.Join("launch", "dir")
	if got := runnerDir(&runner{taskID: task.ID, dir: launch}, task); got != launch {
		t.Errorf("runnerDir = %q, want %q", got, launch)
	}
	if got := runnerDir(&runner{taskID: task.ID}, task); got != filepath.FromSlash("/somewhere/else") {
		t.Errorf("no launch dir should fall back to the card, got %q", got)
	}
}

// sa96: launched where there is no .git, the session's directory moves into a
// subdirectory that has one and back. Nothing about that is a .git going away.
func TestAWorkerThatMovesIntoARepoAndBackIsLeftAlone(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "src")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	g := newGoneRig(t, root, SubagentTag)
	g.r.spec = nil
	g.r.dir = root
	tick := func() {
		g.d.reapGoneWorktrees()
		g.d.reapGoneWorktrees()
	}
	tick()
	if err := os.WriteFile(filepath.Join(sub, ".git"), []byte("gitdir: x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := g.d.st.SetWorktree(g.task.ID, filepath.ToSlash(sub)); err != nil {
		t.Fatal(err)
	}
	tick()
	if err := g.d.st.SetWorktree(g.task.ID, filepath.ToSlash(root)); err != nil {
		t.Fatal(err)
	}
	tick()
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times about a worker that only changed directory", n)
	}
}

// The launch directory really going away still ends it, with no resume spec.
func TestALaunchDirectoryRemovedStillWindsDownWithoutASpec(t *testing.T) {
	dir := liveDir(t)
	g := newGoneRig(t, dir, SubagentTag)
	g.r.spec = nil
	g.r.dir = dir
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	g.d.reapGoneWorktrees()
	g.d.reapGoneWorktrees()
	g.waitAsked(t, 1)
}

// A launch directory that had a .git and lost it is still gone, with no spec.
func TestALaunchDirectoryThatLostItsGitStillWindsDown(t *testing.T) {
	dir := liveDir(t)
	g := newGoneRig(t, dir, SubagentTag)
	g.r.spec = nil
	g.r.dir = dir
	g.d.reapGoneWorktrees()
	if err := os.Remove(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	g.d.reapGoneWorktrees()
	g.d.reapGoneWorktrees()
	g.waitAsked(t, 1)
}

// A .git seen in one directory never makes another read as one that lost it.
func TestGitSeenInOneDirectoryDoesNotCountForAnother(t *testing.T) {
	with := liveDir(t)
	without := t.TempDir()
	g := newGoneRig(t, with, SubagentTag)
	g.r.spec = nil
	g.r.dir = with
	g.d.reapGoneWorktrees()
	g.r.dir = without
	for i := 0; i < 3; i++ {
		g.d.reapGoneWorktrees()
	}
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times", n)
	}
}

// A permission request, an activity report and a session hook, all carrying a
// cwd in a subdirectory, leave the card where it was launched.
func TestHooksCarryingASubdirectoryDoNotMoveTheCard(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{WireName: "stays", Worktree: "/launch/root", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetAutoApprove(task.ID, true); err != nil {
		t.Fatal(err)
	}
	check := func(what string) {
		t.Helper()
		got, err := d.st.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if got.Worktree != "/launch/root" {
			t.Fatalf("after %s the worktree is %q", what, got.Worktree)
		}
	}
	if _, _, err := d.onPermRequest(PermissionRequest{
		Agent: "stays", Tool: "Bash", Command: "ls", Cwd: "/launch/root/src",
	}); err != nil {
		t.Fatal(err)
	}
	check("a permission request")
	d.onActivity(ActivityEvent{Agent: "stays", Event: "tool-start", Tool: "Bash"})
	check("an activity report")
	if err := d.onSession(SessionEvent{Agent: "stays", TaskID: task.ID, Event: "start", Cwd: "/launch/root/src"}); err != nil {
		t.Fatal(err)
	}
	check("a session hook")
	if err := d.onSession(SessionEvent{Agent: "stays", Event: "start", Cwd: "/launch/root/src/deeper"}); err != nil {
		t.Fatal(err)
	}
	check("a session hook by name")
}
