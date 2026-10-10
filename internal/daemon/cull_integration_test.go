//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// cullRepo is a repository with claude/main and one worker's worktree on its
// own branch, holding one commit and atrium's untracked BRIEF.md. merged puts
// the worker's commit on claude/main.
type cullRepo struct {
	main, wt, branch string
}

func cullGit(t *testing.T, dir string, args ...string) string {
	t.Helper()
	full := append([]string{"-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false"}, args...)
	cmd := exec.Command("git", full...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

func newCullRepo(t *testing.T, merged bool) cullRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git is not on PATH")
	}
	root := t.TempDir()
	r := cullRepo{main: filepath.Join(root, "repo"), wt: filepath.Join(root, "wt"), branch: "claude/w1"}
	if err := os.MkdirAll(r.main, 0o755); err != nil {
		t.Fatal(err)
	}
	cullGit(t, r.main, "init", "-q", "-b", "main")
	cullGit(t, r.main, "commit", "-q", "--allow-empty", "-m", "root")
	cullGit(t, r.main, "branch", "claude/main")
	cullGit(t, r.main, "worktree", "add", "-q", "-b", r.branch, r.wt, "claude/main")
	if err := os.WriteFile(filepath.Join(r.wt, "work.txt"), []byte("done\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cullGit(t, r.wt, "add", "work.txt")
	cullGit(t, r.wt, "commit", "-q", "-m", "the work")
	if err := os.WriteFile(filepath.Join(r.wt, briefFile), []byte("brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if merged {
		cullGit(t, r.main, "branch", "-f", "claude/main", r.branch)
	}
	return r
}

func (r cullRepo) branchExists(t *testing.T) bool {
	cmd := exec.Command("git", "rev-parse", "--verify", "-q", "refs/heads/"+r.branch)
	cmd.Dir = r.main
	return cmd.Run() == nil
}

// cullCard is a finished card in dir, tagged as given.
func cullCard(t *testing.T, d *Daemon, dir string, tags ...string) *store.Task {
	t.Helper()
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
	// The automatic cull is off unless the operator sets a grace. These tests are about what it does when it is on.
	if err := d.st.SetSetting(SettingMergedCullGrace, "1800"); err != nil {
		t.Fatal(err)
	}
	return task
}

// Merged, clean but for BRIEF.md, and tagged: the worktree and branch go.
func TestCullRemovesAMergedWorkersWorktreeAndBranch(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.WorktreeRemoved || !res.BranchDeleted || res.Kept != "" {
		t.Fatalf("result = %+v, want the worktree removed and the branch deleted", res)
	}
	if _, err := os.Stat(r.wt); !os.IsNotExist(err) {
		t.Errorf("the worktree is still on disk: %v", err)
	}
	if r.branchExists(t) {
		t.Error("the branch is still there")
	}
	if res.Branch != r.branch || res.Into != DefaultCullInto {
		t.Errorf("branch %q into %q, want %q into %q", res.Branch, res.Into, r.branch, DefaultCullInto)
	}
}

// A cull takes the card off the board with an event saying why, and never files
// it as dead: that would put a death in the ledger for accepted work.
func TestCullArchivesTheCardWithoutKillingIt(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	if _, err := d.Cull(task.ID, ""); err != nil {
		t.Fatalf("cull: %v", err)
	}
	got, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt == nil {
		t.Fatal("the culled card is still on the board")
	}
	if got.Status != store.StatusDone {
		t.Errorf("status = %s, want done: a cull is not a death", got.Status)
	}
	evs, err := d.st.Events(task.ID, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), "culled") {
			found = true
		}
	}
	if !found {
		t.Error("no event says why the card was archived")
	}
}

// An unmerged branch refuses the whole cull and touches nothing.
func TestCullRefusesAnUnmergedBranch(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, false)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	_, err := d.Cull(task.ID, "")
	if err == nil || !strings.Contains(err.Error(), "not merged") {
		t.Fatalf("err = %v, want a refusal naming the unmerged branch", err)
	}
	if _, err := os.Stat(filepath.Join(r.wt, "work.txt")); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
	if !r.branchExists(t) {
		t.Error("the branch was deleted")
	}
}

// Uncommitted changes keep the worktree and the branch, and say why.
func TestCullKeepsADirtyWorktree(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	if err := os.WriteFile(filepath.Join(r.wt, "notes.txt"), []byte("unsaved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if res.WorktreeRemoved || res.BranchDeleted || !strings.Contains(res.Kept, "notes.txt") {
		t.Fatalf("result = %+v, want both kept with notes.txt named", res)
	}
	if _, err := os.Stat(filepath.Join(r.wt, "notes.txt")); err != nil {
		t.Errorf("the uncommitted file is gone: %v", err)
	}
	if !r.branchExists(t) {
		t.Error("the branch was deleted")
	}
}

// A card without atrium:subagent is never culled, whoever asks.
func TestCullRefusesACardNotTaggedSubagent(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag)

	if _, err := d.Cull(task.ID, ""); err == nil || !strings.Contains(err.Error(), SubagentTag) {
		t.Fatalf("err = %v, want a refusal naming %s", err, SubagentTag)
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}

// The main checkout is never removed, even on a worker's card.
func TestCullRefusesTheMainCheckout(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	cullGit(t, r.main, "checkout", "-q", "-b", "feature")
	task := cullCard(t, d, r.main, OriginAgentTag, SubagentTag)

	if _, err := d.Cull(task.ID, ""); err == nil || !strings.Contains(err.Error(), "main checkout") {
		t.Fatalf("err = %v, want a refusal for the main checkout", err)
	}
}

// A card whose work is not done is not reclaimed, and a live session atrium does not own could not be asked to
// leave anyway, so nothing is removed from under it. H4: it stays where the operator can see it.
func TestCullRefusesALiveSessionItDoesNotOwn(t *testing.T) {
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	if _, _, err := d.st.Register(store.Observed{
		WireName: "worker", Worktree: filepath.ToSlash(r.wt), Runner: "claude", PID: os.Getpid(),
	}); err != nil {
		t.Fatal(err)
	}

	if _, err := d.Cull(task.ID, ""); err == nil || !strings.Contains(err.Error(), "its work is not done") {
		t.Fatalf("err = %v, want a refusal for work that is not done", err)
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was removed from under a live session: %v", err)
	}
}

// A supervised worker is asked to leave first, then its worktree and branch go.
func TestCullExitsASupervisedWorkerFirst(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	r := newCullRepo(t, true)
	task, err := d.Launch(LaunchRequest{
		Harness: slowHarness(t, d), Cwd: filepath.ToSlash(r.wt), Tags: []string{SubagentTag},
	})
	if err != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", err)
	}
	if d.sup.get(task.ID) == nil {
		t.Skip("the slow test runner did not stay up long enough to supervise")
	}
	// It reported done and its launcher accepted, which is what a cull is for.
	if err := d.st.SetTags(task.ID, []string{OriginAgentTag, SubagentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}

	res, err := d.Cull(task.ID, "")
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.Exited || !res.WorktreeRemoved || !res.BranchDeleted {
		t.Fatalf("result = %+v, want exited, worktree removed and branch deleted", res)
	}
	if d.sup.get(task.ID) != nil {
		t.Error("the runner is still supervised")
	}
}

func TestProtectedBranches(t *testing.T) {
	for _, b := range []string{"main", "master", "claude/main", "Release"} {
		if !protectedBranch(b, "release") {
			t.Errorf("%s is not protected", b)
		}
	}
	if protectedBranch("claude/w1", "claude/main") {
		t.Error("a worker's branch is protected")
	}
}
