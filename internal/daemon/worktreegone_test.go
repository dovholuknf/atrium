package daemon

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// goneRig is a daemon with one runner entry whose ask is counted rather than
// carried out.
type goneRig struct {
	d     *Daemon
	task  *store.Task
	r     *runner
	asked *atomic.Int32
}

func newGoneRig(t *testing.T, dir string, tags ...string) goneRig {
	t.Helper()
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{
		WireName: "worker", Worktree: filepath.ToSlash(dir), Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(task.ID, tags); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	r := &runner{taskID: task.ID, done: make(chan struct{}), spec: &launchSpec{cwd: dir}}
	d.sup.add(r)
	asked := &atomic.Int32{}
	d.gone.ask = func(*runner) { asked.Add(1) }
	return goneRig{d: d, task: task, r: r, asked: asked}
}

// A checkout with a .git entry, and one without.
func liveDir(t *testing.T) string {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("gitdir: elsewhere\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func (g goneRig) waitAsked(t *testing.T, want int32) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if g.asked.Load() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	if got := g.asked.Load(); got != want {
		t.Fatalf("asked %d times, want %d", got, want)
	}
}

// Gone on one tick is not enough. Gone on the second asks, once, and records why.
func TestARemovedWorktreeIsActedOnAtTheSecondTick(t *testing.T) {
	dir := liveDir(t)
	g := newGoneRig(t, dir, SubagentTag)
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}

	g.d.reapGoneWorktrees()
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked after one tick (%d)", n)
	}
	g.d.reapGoneWorktrees()
	g.waitAsked(t, 1)

	// A third tick while the first ask has already returned would ask again, so
	// what is pinned is only that nothing is recorded twice for one detection.
	evs, err := g.d.st.Events(g.task.ID, 100)
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, e := range evs {
		if e.Kind == store.EventNotified {
			n++
		}
	}
	if n != 1 {
		t.Errorf("%d notified events, want 1", n)
	}
	if got, _ := g.d.st.Get(g.task.ID); got.Status != store.StatusDone {
		t.Errorf("status = %s, want done until the process really exits", got.Status)
	}
}

// A directory that HAD a .git and lost it is what git leaves behind.
func TestADirectoryThatHadGitAndLostItIsGoneOnTheSecondTick(t *testing.T) {
	dir := liveDir(t)
	g := newGoneRig(t, dir, SubagentTag)
	g.d.reapGoneWorktrees() // .git seen
	if err := os.Remove(filepath.Join(dir, ".git")); err != nil {
		t.Fatal(err)
	}
	g.d.reapGoneWorktrees() // first sighting
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked after one tick (%d)", n)
	}
	g.d.reapGoneWorktrees()
	g.waitAsked(t, 1)
}

// A worker launched in a subdirectory never had a .git of its own, so the lack
// of one says nothing. Only the directory disappearing ends it.
func TestADirectoryThatNeverHadGitIsLeftAloneUntilItIsDeleted(t *testing.T) {
	dir := t.TempDir()
	g := newGoneRig(t, dir, SubagentTag)
	for i := 0; i < 4; i++ {
		g.d.reapGoneWorktrees()
	}
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times about a directory that never had a .git", n)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	g.d.reapGoneWorktrees()
	g.d.reapGoneWorktrees()
	g.waitAsked(t, 1)
}

// A worktree that is there is never touched, however many ticks pass.
func TestALiveWorktreeIsLeftAlone(t *testing.T) {
	g := newGoneRig(t, liveDir(t), SubagentTag)
	for i := 0; i < 4; i++ {
		g.d.reapGoneWorktrees()
	}
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times about a live worktree", n)
	}
}

// Only atrium:subagent cards. A human's own terminal is not this job's.
func TestACardNotTaggedSubagentIsLeftAlone(t *testing.T) {
	g := newGoneRig(t, filepath.Join(t.TempDir(), "missing"), OriginAgentTag)
	for i := 0; i < 3; i++ {
		g.d.reapGoneWorktrees()
	}
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times about an untagged card", n)
	}
}

// A shell is not in `runners`, so it is never looked at.
func TestAShellIsLeftAlone(t *testing.T) {
	g := newGoneRig(t, filepath.Join(t.TempDir(), "missing"), SubagentTag)
	g.d.sup.remove(g.r.taskID)
	g.d.sup.mu.Lock()
	if g.d.sup.shells == nil {
		g.d.sup.shells = map[string]*runner{}
	}
	g.d.sup.shells[g.r.taskID] = g.r
	g.d.sup.mu.Unlock()
	for i := 0; i < 3; i++ {
		g.d.reapGoneWorktrees()
	}
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times about a shell", n)
	}
}

// A directory that comes back between ticks resets the guard.
func TestADirectoryThatComesBackResetsTheGuard(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "wt")
	g := newGoneRig(t, dir, SubagentTag)

	g.d.reapGoneWorktrees() // missing: first sighting
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, ".git"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	g.d.reapGoneWorktrees() // back: guard cleared
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
	g.d.reapGoneWorktrees() // missing again: a first sighting, not a second
	time.Sleep(50 * time.Millisecond)
	if n := g.asked.Load(); n != 0 {
		t.Fatalf("asked %d times across a reappearance", n)
	}
	g.d.reapGoneWorktrees()
	g.waitAsked(t, 1)
}
