//go:build integration

package daemon

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func landOnMainByPick(t *testing.T, r cullRepo) {
	t.Helper()
	cullGit(t, r.main, "switch", "-q", "claude/main")
	cullGit(t, r.main, "cherry-pick", "-x", r.branch)
	cullGit(t, r.main, "switch", "-q", "main")
}

func idleLanded(t *testing.T, d *Daemon, r cullRepo, tags ...string) *store.Task {
	t.Helper()
	if len(tags) == 0 {
		tags = []string{OriginAgentTag, SubagentTag}
	}
	task := cullCard(t, d, r.wt, tags...)
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestCullTakesABranchLandedByPatchIDThatIsNotAnAncestor(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	landOnMainByPick(t, r)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.WorktreeRemoved || !res.BranchDeleted {
		t.Fatalf("result = %+v, want the worktree removed and the branch deleted", res)
	}
	if r.branchExists(t) {
		t.Error("the branch is still there")
	}
}

func TestCullRefusesABranchWithAPatchMainLacks(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	if _, err := d.Cull(task.ID, ""); err == nil || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("err = %v, want a refusal saying it is not merged", err)
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}

func TestTheSweepReclaimsAnIdleLandedWorkerThatNeverSaidDone(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	landOnMainByPick(t, r)
	task := idleLanded(t, d, r)

	d.landedPass(time.Hour, time.Now().Add(2*time.Hour))

	if _, err := os.Stat(r.wt); !os.IsNotExist(err) {
		t.Errorf("the worktree is still on disk: %v", err)
	}
	if r.branchExists(t) {
		t.Error("the branch is still there")
	}
	got, _ := d.st.Get(task.ID)
	if got.ArchivedAt == nil {
		t.Error("the card was not taken off the board")
	}
}

func TestTheSweepLeavesALandedWorkerThatIsNotIdleYet(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	landOnMainByPick(t, r)
	idleLanded(t, d, r)

	d.landedPass(time.Hour, time.Now())

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}

func TestTheSweepKeepsALandedWorkerTaggedToStayAndSaysWhy(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	landOnMainByPick(t, r)
	task := idleLanded(t, d, r, OriginAgentTag, SubagentTag, KeepOpenTag)

	d.landedPass(time.Hour, time.Now().Add(2*time.Hour))

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
	why, _ := d.landedKeptFor(task.ID).(string)
	if !strings.Contains(why, KeepOpenTag) {
		t.Errorf("landed_kept = %q, want it to name %s", why, KeepOpenTag)
	}
}

func TestTheSweepKeepsALandedWorkerWithADirtyWorktreeAndSaysWhy(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	landOnMainByPick(t, r)
	task := idleLanded(t, d, r)
	if err := os.WriteFile(r.wt+"/scratch.txt", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	d.landedPass(time.Hour, time.Now().Add(2*time.Hour))

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
	got, _ := d.st.Get(task.ID)
	if got.ArchivedAt != nil {
		t.Error("a card with a dirty worktree was taken off the board")
	}
	if why, _ := d.landedKeptFor(task.ID).(string); !strings.Contains(why, "uncommitted") {
		t.Errorf("landed_kept = %q, want it to say uncommitted", why)
	}
}

func TestTheSweepNeverTouchesACardTheOperatorStarted(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	landOnMainByPick(t, r)
	idleLanded(t, d, r, SubagentTag)

	d.landedPass(time.Hour, time.Now().Add(2*time.Hour))

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}
