//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func linkDir(t *testing.T, target, link string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		if out, err := exec.Command("cmd", "/c", "mklink", "/J", link, target).CombinedOutput(); err != nil {
			t.Skipf("no junction here: %v %s", err, out)
		}
		return
	}
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("no symlink here: %v", err)
	}
}

func sharedModules(t *testing.T) (dir, sentinel string) {
	t.Helper()
	dir = filepath.Join(t.TempDir(), "node_modules")
	if err := os.MkdirAll(filepath.Join(dir, "pkg"), 0o755); err != nil {
		t.Fatal(err)
	}
	sentinel = filepath.Join(dir, "pkg", "index.js")
	if err := os.WriteFile(sentinel, []byte("shared\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir, sentinel
}

func lastNotes(t *testing.T, d *Daemon, id string) string {
	t.Helper()
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	var b strings.Builder
	for _, e := range evs {
		b.WriteString(string(e.Payload))
		b.WriteString("\n")
	}
	return b.String()
}

func TestPruneRemovesAJunctionAsALinkAndNeverTouchesItsTarget(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	shared, sentinel := sharedModules(t)
	linkDir(t, shared, filepath.Join(r.wt, "node_modules"))
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.WorktreeRemoved || !res.BranchDeleted || res.Kept != "" {
		t.Fatalf("result = %+v, want the worktree removed and the branch deleted", res)
	}
	if _, err := os.Lstat(r.wt); !os.IsNotExist(err) {
		t.Errorf("the worktree is still on disk: %v", err)
	}
	if _, err := os.Stat(sentinel); err != nil {
		t.Errorf("the shared node_modules was touched: %v", err)
	}
}

func TestPruneCountsABranchLandedOnTheHubsCopyOnly(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	cullGit(t, r.main, "switch", "-q", "-c", "tmp", "claude/main")
	cullGit(t, r.main, "cherry-pick", "-x", r.branch)
	tip := cullGit(t, r.main, "rev-parse", "HEAD")
	cullGit(t, r.main, "switch", "-q", "main")
	cullGit(t, r.main, "update-ref", "refs/remotes/hub/claude/main", tip)
	cullGit(t, r.main, "branch", "-D", "tmp")
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.WorktreeRemoved || !res.BranchDeleted {
		t.Fatalf("result = %+v, want the rewritten branch pruned", res)
	}
}

func TestPruneKeepsADirtyWorktreeAndSaysSoOnTheCard(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	if err := os.WriteFile(filepath.Join(r.wt, "scratch.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := idleLanded(t, d, r)

	d.landedPass(time.Minute, time.Now().Add(time.Hour))

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("a dirty worktree was removed: %v", err)
	}
	if !r.branchExists(t) {
		t.Error("the branch of a dirty worktree was deleted")
	}
	if notes := lastNotes(t, d, task.ID); !strings.Contains(notes, "uncommitted") {
		t.Errorf("the card does not say why it was kept: %s", notes)
	}
}

func TestPruneWritesOneLineOnTheCardForAPrune(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := idleLanded(t, d, r)

	d.landedPass(time.Minute, time.Now().Add(time.Hour))

	if _, err := os.Stat(r.wt); !os.IsNotExist(err) {
		t.Fatalf("the worktree is still on disk: %v", err)
	}
	if notes := lastNotes(t, d, task.ID); !strings.Contains(notes, "pruned") {
		t.Errorf("the card has no prune line: %s", notes)
	}
}

func TestPruneGraceStartsWhenTheRunnerExits(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := idleLanded(t, d, r)
	now := time.Now().Add(time.Hour)
	d.landed.exited(task.ID, now.Add(-time.Minute))

	d.landedPass(5*time.Minute, now)

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("pruned before the grace after the exit: %v", err)
	}
	d.landed.checked = nil
	d.landedPass(5*time.Minute, now.Add(6*time.Minute))
	if _, err := os.Stat(r.wt); !os.IsNotExist(err) {
		t.Errorf("not pruned after the grace: %v", err)
	}
}

func TestPruneTakesAWorkItemCardLaunchedWithoutTheAgentTags(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := idleLanded(t, d, r, ItemTag)
	if err := d.st.SetLifetime(task.ID, store.LifetimeWork); err != nil {
		t.Fatal(err)
	}

	d.landedPass(time.Minute, time.Now().Add(time.Hour))

	if _, err := os.Stat(r.wt); !os.IsNotExist(err) {
		t.Errorf("an item card was not pruned: %v", err)
	}
}

func TestPruneGraceDefaultsToMinutes(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	g, on := d.landedGrace()
	if !on || g <= 0 || g > time.Hour {
		t.Fatalf("grace = %v on=%v, want a few minutes", g, on)
	}
}
