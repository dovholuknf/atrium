//go:build integration

package daemon

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// ledgerWorker is launchedPair with the worker's item made, as a launch makes
// it.
func ledgerWorker(t *testing.T, d *Daemon) (launcher, worker *store.Task) {
	t.Helper()
	launcher, worker = launchedPair(t, d)
	worker, _ = d.st.Get(worker.ID)
	if _, err := d.st.CreateWorkItem(worker, store.NewWorkItem{Brief: "do the thing"}); err != nil {
		t.Fatal(err)
	}
	return launcher, worker
}

func itemOf(t *testing.T, d *Daemon, id string) *store.WorkItem {
	t.Helper()
	w, err := d.st.WorkItem(id)
	if err != nil {
		t.Fatalf("no work item for %s: %v", id, err)
	}
	return w
}

// The store's `origin:agent` and the daemon's are one string.
func TestTheLedgerAndTheDaemonAgreeOnTheAgentTag(t *testing.T) {
	if OriginAgentTag != "origin:agent" {
		t.Fatalf("OriginAgentTag is %q, and the backfill reads origin:agent", OriginAgentTag)
	}
}

// A worker's done moves its card to done and its work to reported, which is
// not finished. The launcher is told once, from the same transaction.
func TestADoneReportIsReportedNotFinished(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := ledgerWorker(t, d)

	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "research only",
		Recap: "the answer is 42"})
	if rec.Code != 200 {
		t.Fatalf("finish answered %d: %s", rec.Code, rec.Body)
	}
	if out["work_state"] != store.WorkReported || out["launcher_told"] != true {
		t.Fatalf("finish said %v", out)
	}
	card, _ := d.st.Get(worker.ID)
	if card.Status != store.StatusDone {
		t.Fatalf("card is in %s, want done: the column is the session", card.Status)
	}
	w := itemOf(t, d, worker.ID)
	if w.State != store.WorkReported || w.Outputs.NoCommit != "research only" {
		t.Fatalf("item = %+v", w)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "the answer is 42") {
		t.Fatalf("launcher queue = %+v", msgs)
	}
}

// A card nobody gave an item still reports as it always has, and its launcher
// still hears about it.
func TestAReportWithNoWorkItemStillReachesTheLauncher(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	_, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Recap: "half"})
	if out["launcher_told"] != true || out["work_state"] != nil {
		t.Fatalf("finish said %v", out)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 {
		t.Fatalf("launcher queue = %+v", msgs)
	}
}

// A launch makes the item with its brief. A runner that falls over at once is
// noticed by two exit paths, the supervisor and the launch, and is one death:
// one row, one notice.
func TestALaunchMakesTheItemAndAFailedStartEndsItOnce(t *testing.T) {
	d := testDaemon(t)
	launcher := peerCard(t, d, "orchestrator")
	t.Setenv("ATRIUM_TEST_SOURCE", "1")
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "quick", Label: "quick", Enabled: true, LaunchMode: store.LaunchPTY,
		Cmd: os.Args[0], Args: []string{"-test.run=TestHelperSourceProcess"},
		PromptArgs: []string{"-test.v={prompt}"},
	}); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	_, launchErr := d.Launch(LaunchRequest{
		Harness: "quick", Cwd: dir, Title: "sa99", Brief: "the brief", Prompt: "go",
		Tags: []string{OriginAgentTag}, SpawnedBy: "orchestrator", Scratch: true,
	})
	if launchErr == nil {
		t.Fatal("the quick runner should have failed to settle")
	}
	items, err := d.st.Ledger()
	if err != nil {
		t.Fatal(err)
	}
	if len(items.Open) != 1 {
		t.Fatalf("ledger = %+v, launch said %v", items.Open, launchErr)
	}
	w := items.Open[0]
	// The supervisor's exit may still be on its way. Give it a moment, then
	// count: however many paths saw the death, it is one.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && d.sup.get(w.TaskID) != nil {
		time.Sleep(20 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	w = itemOf(t, d, w.TaskID)
	if w.State != store.WorkEnded || w.LauncherID != launcher.ID || !strings.Contains(w.Brief, "the brief") ||
		!strings.HasSuffix(w.BriefPath, "BRIEF.md") {
		t.Fatalf("item = %+v", w)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 {
		t.Fatalf("one failed start sent %d notices", len(msgs))
	}
	log, _ := d.st.WorkLog(w.TaskID, 0)
	ended := 0
	for _, e := range log {
		if e.Kind == store.LogAtrium && strings.Contains(e.Text, "ended") {
			ended++
		}
	}
	if ended != 1 {
		t.Fatalf("one death wrote %d ended rows: %+v", ended, log)
	}
}

// A board launch gets no item.
func TestABoardLaunchGetsNoWorkItem(t *testing.T) {
	d := testDaemon(t)
	h := briefHarness(t, d)
	// Fails at the spawn, before any item could be made, and a board launch
	// would not make one anyway. What is checked is that nothing appears.
	_, _ = d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), SpawnedBy: store.HumanLauncher})
	v, err := d.st.Ledger()
	if err != nil || len(v.Open) != 0 {
		t.Fatalf("ledger = %+v err=%v", v, err)
	}
}

// deadPID is a process id that has just exited.
func deadPID(t *testing.T) int {
	t.Helper()
	c := exec.Command(os.Args[0], "-test.run=^$")
	if err := c.Run(); err != nil {
		t.Fatal(err)
	}
	return c.Process.Pid
}

// The sweep ends work whose process is gone with no exit recorded, and leaves
// work whose liveness it cannot know exactly where it is.
func TestTheSweepEndsGoneWorkAndLeavesUnknownAlone(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := ledgerWorker(t, d)
	if _, _, err := d.st.Register(store.Observed{WireName: "worker", Worktree: "/tmp/worker", Runner: "claude",
		PID: deadPID(t)}); err != nil {
		t.Fatal(err)
	}
	// A waiting worker with no pid, quiet past the window, that atrium does
	// not own: unknown.
	quiet := peerCard(t, d, "quiet")
	if err := d.st.SetTags(quiet.ID, []string{OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(quiet.ID, "orchestrator", launcher.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(quiet.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	quiet, _ = d.st.Get(quiet.ID)
	quiet.LastActivityAt = time.Now().Add(-2 * QuietAfter)
	if got := d.liveness(quiet); got != store.LiveUnknown {
		t.Fatalf("a quiet waiting card with no pid reads %s, want unknown", got)
	}
	if _, err := d.st.CreateWorkItem(quiet, store.NewWorkItem{}); err != nil {
		t.Fatal(err)
	}

	if err := d.sweepLedger(); err != nil {
		t.Fatal(err)
	}
	if w := itemOf(t, d, worker.ID); w.State != store.WorkEnded {
		t.Fatalf("gone worker's item = %s", w.State)
	}
	if w := itemOf(t, d, quiet.ID); w.State != store.WorkOpen {
		t.Fatalf("a card of unknown liveness moved to %s", w.State)
	}
	// Again, as the next tick: nothing more happens.
	if err := d.sweepLedger(); err != nil {
		t.Fatal(err)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 1 {
		t.Fatalf("two sweeps sent %d notices", len(msgs))
	}
}

// What a worker and its launcher say to each other is on the worker's item.
func TestPeerMessagesLandOnTheWork(t *testing.T) {
	d := testDaemon(t)
	_, worker := ledgerWorker(t, d)
	if _, code := tell(t, d, "worker", "orchestrator", "halfway, tests next"); code != 200 {
		t.Fatalf("tell answered %d", code)
	}
	if _, code := tell(t, d, "orchestrator", "worker", "also update the changelog"); code != 200 {
		t.Fatalf("tell answered %d", code)
	}
	log, err := d.st.WorkLog(worker.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, e := range log {
		kinds = append(kinds, e.Kind+":"+e.Text)
	}
	got := strings.Join(kinds, "|")
	if !strings.Contains(got, "say:halfway, tests next") || !strings.Contains(got, "instruction:also update the changelog") {
		t.Fatalf("log = %s", got)
	}
}

// The snapshot is written beside the database on start and on change, and a
// write that cannot happen fails nothing.
func TestTheSnapshotFileFollowsTheLedger(t *testing.T) {
	d := testDaemon(t)
	_, worker := ledgerWorker(t, d)
	d.startLedger()
	raw, err := os.ReadFile(d.ledgerFile())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), worker.ID) {
		t.Fatalf("snapshot does not list the item:\n%s", raw)
	}
	// A change asks for a rewrite and never waits for it.
	select {
	case <-d.ledgerDirty:
	default:
	}
	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "none needed"})
	select {
	case <-d.ledgerDirty:
	default:
		t.Fatal("a report did not ask for the snapshot to be rewritten")
	}
	d.writeLedgerFile()
	raw, _ = os.ReadFile(d.ledgerFile())
	if !strings.Contains(string(raw), "Reported, waiting on a verdict") {
		t.Fatalf("snapshot did not follow the report:\n%s", raw)
	}
	// A directory where the file should be: logged, and nothing fails.
	if err := os.Remove(d.ledgerFile()); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(d.ledgerFile(), 0o755); err != nil {
		t.Fatal(err)
	}
	d.writeLedgerFile()
	if _, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportProgress, Recap: "more"}); out["ok"] != true {
		t.Fatalf("a report failed because the snapshot could not be written: %v", out)
	}
}
