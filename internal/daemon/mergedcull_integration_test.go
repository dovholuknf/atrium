//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// mergedWorker is a finished worker in r's worktree, launched by an
// orchestrator, whose last report was done.
func mergedWorker(t *testing.T, d *Daemon, r cullRepo, tags ...string) (launcher, worker *store.Task) {
	t.Helper()
	launcher = peerCard(t, d, "orchestrator")
	if len(tags) == 0 {
		tags = []string{OriginAgentTag, SubagentTag}
	}
	worker = cullCard(t, d, r.wt, tags...)
	if err := d.st.SetLineage(worker.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLifetime(worker.ID, store.LifetimeWork); err != nil {
		t.Fatal(err)
	}
	worker, _ = d.st.Get(worker.ID)
	if _, err := d.st.CreateWorkItem(worker, store.NewWorkItem{Brief: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "merged",
		Recap: "done"}); rec.Code != 200 {
		t.Fatalf("finish answered %d: %s", rec.Code, rec.Body)
	}
	return launcher, worker
}

func markedMerged(t *testing.T, d *Daemon, into string, branches ...string) *MergedResult {
	t.Helper()
	res, err := d.Merged(into, branches)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func TestAMergeMarksAFinishedWorkerAndTellsItsLauncherOnce(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	launcher, worker := mergedWorker(t, d, r)

	// The report's own message to the launcher is not the one under test.
	before := len(pendingFrom(t, d, launcher.ID))
	res := markedMerged(t, d, DefaultCullInto)
	if len(res.Marked) != 1 || res.Marked[0].Card != worker.ID {
		t.Fatalf("result = %+v, want the worker marked", res)
	}
	w := itemOf(t, d, worker.ID)
	if w.CullAt == nil || time.Until(*w.CullAt) < 25*time.Minute {
		t.Fatalf("cull_at = %v, want about the default grace from now", w.CullAt)
	}
	v, err := d.st.MergedViewFor(worker.ID)
	if err != nil || v == nil || v.Into != DefaultCullInto || v.Branch != r.branch {
		t.Fatalf("view = %+v err=%v", v, err)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != before+1 || !strings.Contains(msgs[len(msgs)-1].Text, "merged into "+DefaultCullInto) ||
		!strings.Contains(msgs[len(msgs)-1].Text, "hold=true") {
		t.Fatalf("launcher messages = %+v", msgs)
	}
	// The same merge again is not a second message, and does not move the time.
	res = markedMerged(t, d, DefaultCullInto)
	if len(res.Marked) != 0 {
		t.Fatalf("a second merge marked again: %+v", res)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != before+1 {
		t.Fatalf("launcher has %d messages after a second merge", n)
	}
}

// When the time comes the room culls it with every check re-run, accepts the
// work, and archives the card.
func TestADueMarkCullsAcceptsAndArchives(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	markedMerged(t, d, DefaultCullInto)

	w := itemOf(t, d, worker.ID)
	d.cullDue(w)

	if _, err := os.Stat(r.wt); !os.IsNotExist(err) {
		t.Errorf("the worktree is still there: %v", err)
	}
	if r.branchExists(t) {
		t.Error("the branch is still there")
	}
	if got := itemOf(t, d, worker.ID); got.State != store.WorkAccepted {
		t.Errorf("work state = %s, want accepted", got.State)
	}
	if card, _ := d.st.Get(worker.ID); card.ArchivedAt == nil {
		t.Error("the card is still on the board")
	}
}

func TestANewTurnBeforeTheTimeClearsTheMark(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	markedMerged(t, d, DefaultCullInto)

	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if w := itemOf(t, d, worker.ID); w.CullAt != nil {
		t.Fatalf("the mark survived a new turn: %v", w.CullAt)
	}
	if due, _ := d.st.DueCulls(time.Now().Add(24 * time.Hour)); len(due) != 0 {
		t.Fatalf("%d due after a new turn", len(due))
	}
}

func TestAHeldWorkerIsNeverMarkedOrCulled(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	if err := d.HoldCull(worker.ID, "clint"); err != nil {
		t.Fatal(err)
	}
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 0 {
		t.Fatalf("a held worker was marked: %+v", res)
	}
	if due, _ := d.st.DueCulls(time.Now().Add(24 * time.Hour)); len(due) != 0 {
		t.Fatalf("a held worker is due: %+v", due)
	}
}

func TestAHoldAfterTheMarkCancelsIt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	markedMerged(t, d, DefaultCullInto)
	if err := d.HoldCull(worker.ID, "clint"); err != nil {
		t.Fatal(err)
	}
	w := itemOf(t, d, worker.ID)
	if w.CullAt != nil || w.HeldBy != "clint" {
		t.Fatalf("item = %+v, want no mark and held by clint", w)
	}
}

func TestADirectorIsNeverMarked(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	mergedWorker(t, d, r, OriginAgentTag, SubagentTag, DirectorTag)
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 0 {
		t.Fatalf("a director was marked: %+v", res)
	}
}

func TestACardWithoutAWorkerTagIsNeverMarked(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r, "sdk")
	if err := d.st.SetLifetime(worker.ID, "resident"); err != nil {
		t.Fatal(err)
	}
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 0 {
		t.Fatalf("a card with no origin was marked: %+v", res)
	}
}

// origin:agent with a recorded launcher and no director tag is a worker, tag or no tag.
func TestAnAgentLaunchedCardWithALauncherCountsAsAWorker(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r, OriginAgentTag)
	card, _ := d.st.Get(worker.ID)
	if !d.isWorker(card) {
		t.Fatal("origin:agent plus a launcher is not counted as a worker")
	}
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 1 {
		t.Fatalf("result = %+v", res)
	}
}

func TestARunningWorkerIsNotMarkedUntilItIsDone(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	if err := d.st.SetStatus(worker.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 0 {
		t.Fatalf("a running worker was marked: %+v", res)
	}
	if err := d.st.SetStatus(worker.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 1 {
		t.Fatalf("a done worker was not marked: %+v", res)
	}
}

func TestAWorkerWithAnOpenQuestionIsNotMarked(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	if err := d.st.NoteTurnEnded(worker.ID, store.TurnQuestions{Known: true, Block: true, List: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 0 {
		t.Fatalf("a worker with a question was marked: %+v", res)
	}
}

func TestAnUnmergedBranchIsNotMarked(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	mergedWorker(t, d, r)
	if res := markedMerged(t, d, DefaultCullInto); len(res.Marked) != 0 {
		t.Fatalf("an unmerged worker was marked: %+v", res)
	}
}

// A worktree that went dirty during the grace period is kept, not force-removed.
func TestADirtyWorktreeIsKeptWhenTheTimeComes(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	markedMerged(t, d, DefaultCullInto)
	if err := os.WriteFile(r.wt+"/notes.txt", []byte("unsaved\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.cullDue(itemOf(t, d, worker.ID))
	if _, err := os.Stat(r.wt + "/notes.txt"); err != nil {
		t.Fatalf("the uncommitted file is gone: %v", err)
	}
	if !r.branchExists(t) {
		t.Error("the branch was deleted")
	}
}

func TestMergedHonoursTheBranchFilter(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	mergedWorker(t, d, r)
	if res := markedMerged(t, d, DefaultCullInto, "claude/other"); len(res.Marked) != 0 {
		t.Fatalf("a branch not merged was marked: %+v", res)
	}
	if res := markedMerged(t, d, DefaultCullInto, r.branch); len(res.Marked) != 1 {
		t.Fatalf("the merged branch was not marked: %+v", res)
	}
}

func TestMergedGraceOffMarksNothing(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	mergedWorker(t, d, r)
	if err := d.st.SetSetting(SettingMergedCullGrace, "off"); err != nil {
		t.Fatal(err)
	}
	res := markedMerged(t, d, DefaultCullInto)
	if !res.Off || len(res.Marked) != 0 {
		t.Fatalf("result = %+v, want off and nothing marked", res)
	}
}

// The remote room accepts a proof only while its worktree still sits at the tip
// the merge was checked at.
func TestAProvedCullIsRefusedOnceTheWorktreeMoved(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	tip := cullGit(t, r.wt, "rev-parse", "HEAD")

	cullGit(t, r.wt, "commit", "-q", "--allow-empty", "-m", "more")
	_, err := d.CullProved(task.ID, DefaultCullInto, tip)
	if err == nil || !strings.Contains(err.Error(), "new commits") {
		t.Fatalf("err = %v, want a refusal naming new commits", err)
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}

func TestAProvedCullIsAcceptedAtTheProvedTip(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	tip := cullGit(t, r.wt, "rev-parse", "HEAD")
	res, err := d.CullProved(task.ID, DefaultCullInto, tip)
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.WorktreeRemoved {
		t.Fatalf("result = %+v, want the worktree removed on the strength of the proof", res)
	}
}

func TestAProvedCullAcceptsAnAbbreviatedTip(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	tip := cullGit(t, r.wt, "rev-parse", "HEAD")[:7]
	res, err := d.CullProved(task.ID, DefaultCullInto, tip)
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if !res.WorktreeRemoved {
		t.Fatalf("result = %+v, want the worktree removed", res)
	}
}

func TestAProvedCullRefusesATipThatIsNotACommitHere(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	_, err := d.CullProved(task.ID, DefaultCullInto, "deadbeefdeadbeef")
	if err == nil || !strings.Contains(err.Error(), "not a commit in this room") || strings.Contains(err.Error(), "new commits") {
		t.Fatalf("err = %v, want a refusal saying the tip is not a commit here", err)
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}

func TestAProvedCullShowsBothShasAtTheSameLengthWhenMoved(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	tip := cullGit(t, r.wt, "rev-parse", "HEAD")
	cullGit(t, r.wt, "commit", "-q", "--allow-empty", "-m", "more")
	_, err := d.CullProved(task.ID, DefaultCullInto, tip[:7])
	if err == nil || !strings.Contains(err.Error(), "new commits") || !strings.HasSuffix(err.Error(), " vs "+tip[:10]) {
		t.Fatalf("err = %v, want new commits with a 10-character sha on both sides", err)
	}
}

func TestAProvedCullRefusesATipThatIsNotAHexSha(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, false)
	task := cullCard(t, d, r.wt, OriginAgentTag, SubagentTag)
	for _, tip := range []string{"HEAD", "claude/r-026", "-x", "abc12"} {
		_, err := d.CullProved(task.ID, DefaultCullInto, tip)
		if err == nil || !strings.Contains(err.Error(), "is not a sha") {
			t.Fatalf("tip %q: err = %v, want a refusal saying it is not a sha", tip, err)
		}
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
}

// With merged_cull_grace off, a merge marks nothing and a merged worker keeps its worktree and branch.
func TestAMergedWorkerStaysWithTheGraceOff(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	r := newCullRepo(t, true)
	_, worker := mergedWorker(t, d, r)
	// cullCard turns the grace on for the tests about it. Put the room back to a new room's state.
	if err := d.st.SetSetting(SettingMergedCullGrace, "off"); err != nil {
		t.Fatal(err)
	}
	if _, on := d.mergedGrace(); on {
		t.Fatal("merged_cull_grace off reads as on")
	}

	res := markedMerged(t, d, DefaultCullInto)
	if !res.Off || len(res.Marked) != 0 {
		t.Fatalf("result = %+v, want off and nothing marked", res)
	}
	d.sweepMergedCulls()

	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was touched: %v", err)
	}
	if !r.branchExists(t) {
		t.Error("the branch was deleted")
	}
	got, err := d.st.Get(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ArchivedAt != nil {
		t.Error("the merged worker's card was archived")
	}
}

// launchedMerged is a supervised worker in r's worktree that reported done, launched with `lifetime`.
func launchedMerged(t *testing.T, d *Daemon, r cullRepo, lifetime string) *store.Task {
	t.Helper()
	d.hk.stopAfterReport = func(runnerStopper, string) error { return nil }
	launcher := peerCard(t, d, "orchestrator")
	task, err := d.Launch(LaunchRequest{
		Harness: slowHarness(t, d), Cwd: filepath.ToSlash(r.wt), Lifetime: lifetime,
		Tags: []string{OriginAgentTag, SubagentTag},
	})
	if err != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", err)
	}
	if d.sup.get(task.ID) == nil {
		t.Skip("the slow test runner did not stay up long enough to supervise")
	}
	if err := d.st.SetLineage(task.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	task, _ = d.st.Get(task.ID)
	if _, err := d.st.CreateWorkItem(task, store.NewWorkItem{Brief: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := finishWith(t, d, FinishRequest{Agent: task.WireName, Status: ReportDone, NoCommit: "merged",
		Recap: "done"}); rec.Code != 200 {
		t.Fatalf("finish answered %d: %s", rec.Code, rec.Body)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	return task
}

func TestALaunchRecordsItsLifetimeAndRefusesAnUnknownOne(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	r := newCullRepo(t, true)
	if _, err := d.Launch(LaunchRequest{Harness: slowHarness(t, d), Cwd: filepath.ToSlash(r.wt), Lifetime: "forever"}); err == nil ||
		!strings.Contains(err.Error(), "lifetime") {
		t.Fatalf("err = %v, want an unknown lifetime refused", err)
	}
}

// A work card is exited once its branch merges, even with merged_cull_grace off, and only the runner goes.
func TestAWorkCardIsExitedWhenItsBranchMergesWithTheGraceOff(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	r := newCullRepo(t, true)
	task := launchedMerged(t, d, r, store.LifetimeWork)
	if got, _ := d.st.Get(task.ID); got.Lifetime != store.LifetimeWork {
		t.Fatalf("lifetime = %q, want work", got.Lifetime)
	}
	if err := d.st.SetSetting(SettingMergedCullGrace, "off"); err != nil {
		t.Fatal(err)
	}
	res := markedMerged(t, d, DefaultCullInto)
	if len(res.Exited) != 1 || res.Exited[0] != task.ID {
		t.Fatalf("result = %+v, want the work card exited", res)
	}
	if !d.waitRunnerGone(task.ID, restartGoneWait) {
		t.Fatal("the runner is still supervised after the merge")
	}
	if _, err := d.st.Get(task.ID); err != nil {
		t.Errorf("the card is gone: %v", err)
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was removed: %v", err)
	}
}

// A card from atrium_launch has no lifetime and is never exited because of a merge.
func TestAKeptCardIsNotExitedWhenItsBranchMerges(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	r := newCullRepo(t, true)
	task := launchedMerged(t, d, r, "")
	for _, grace := range []string{"off", "1800"} {
		if err := d.st.SetSetting(SettingMergedCullGrace, grace); err != nil {
			t.Fatal(err)
		}
		res := markedMerged(t, d, DefaultCullInto)
		if len(res.Exited) != 0 || len(res.Marked) != 0 {
			t.Fatalf("grace %s: result = %+v, want nothing exited or marked", grace, res)
		}
	}
	if d.sup.get(task.ID) == nil {
		t.Error("the kept card's runner was stopped")
	}
	if _, err := os.Stat(r.wt); err != nil {
		t.Errorf("the worktree was removed: %v", err)
	}
}

// A work card whose branch is not merged keeps running.
func TestAWorkCardWhoseBranchIsNotMergedKeepsRunning(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	r := newCullRepo(t, false)
	task := launchedMerged(t, d, r, store.LifetimeWork)
	if res := markedMerged(t, d, DefaultCullInto); len(res.Exited) != 0 {
		t.Fatalf("result = %+v, want nothing exited", res)
	}
	if d.sup.get(task.ID) == nil {
		t.Error("an unmerged work card was exited")
	}
}
