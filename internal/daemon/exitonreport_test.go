package daemon

import (
	"errors"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// watchExits swaps the exit for a recorder. Each call sends the card id and how many messages the launcher
// already had queued at that moment.
func watchExits(t *testing.T, d *Daemon, launcherID string, fail error) chan [2]any {
	t.Helper()
	oldDelay, oldStop := exitOnReportDelay, stopAfterReport
	exitOnReportDelay = 10 * time.Millisecond
	got := make(chan [2]any, 4)
	stopAfterReport = func(dd *Daemon, id string) error {
		got <- [2]any{id, len(pendingFrom(t, dd, launcherID))}
		return fail
	}
	t.Cleanup(func() { exitOnReportDelay, stopAfterReport = oldDelay, oldStop })
	return got
}

func expectNoExit(t *testing.T, got chan [2]any) {
	t.Helper()
	select {
	case g := <-got:
		t.Fatalf("exit asked for %v, want none", g[0])
	case <-time.After(200 * time.Millisecond):
	}
}

// A spawned card reporting done is asked to leave, and the launcher already holds the report by then.
func TestASpawnedCardReportingDoneIsAskedToLeaveAfterTheLauncherHasTheReport(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	got := watchExits(t, d, launcher.ID, nil)

	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "research",
		Recap: "the answer"}); rec.Code != 200 {
		t.Fatalf("finish answered %d: %s", rec.Code, rec.Body)
	}
	select {
	case g := <-got:
		if g[0] != worker.ID || g[1].(int) != 1 {
			t.Fatalf("exit = %v, want %s with the report already queued", g, worker.ID)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the worker was never asked to leave")
	}
	card, _ := d.st.Get(worker.ID)
	if card.Status != store.StatusDone {
		t.Fatalf("card is in %s, want done", card.Status)
	}
}

// A report that is not final leaves the card running.
func TestAQuestionProgressOrBlockedReportLeavesTheCardRunning(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	got := watchExits(t, d, launcher.ID, nil)

	for _, in := range []FinishRequest{
		{Agent: "worker", Status: ReportQuestion, Ask: "which one?"},
		{Agent: "worker", Status: ReportProgress, Recap: "half"},
		{Agent: "worker", Status: ReportBlocked, Ask: "need a key"},
	} {
		if rec, _ := finishWith(t, d, in); rec.Code != 200 {
			t.Fatalf("%s answered %d: %s", in.Status, rec.Code, rec.Body)
		}
	}
	expectNoExit(t, got)
}

// A director's done report never exits it, and neither does a card nobody launched.
func TestADirectorOrAnUnlaunchedCardIsNeverExitedByItsReport(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	got := watchExits(t, d, launcher.ID, nil)

	if err := d.st.SetTags(worker.ID, []string{OriginAgentTag, DirectorTag}); err != nil {
		t.Fatal(err)
	}
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "x"}); rec.Code != 200 {
		t.Fatalf("director report answered %d: %s", rec.Code, rec.Body)
	}
	expectNoExit(t, got)

	// The launcher itself has no launcher.
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "orchestrator", Status: ReportDone}); rec.Code != 200 {
		t.Fatalf("launcher report answered %d: %s", rec.Code, rec.Body)
	}
	expectNoExit(t, got)
}

// A failed exit is logged and the report still landed.
func TestAFailedExitAfterAReportLeavesTheReportLanded(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	got := watchExits(t, d, launcher.ID, errors.New("no terminal"))

	finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, NoCommit: "x", Recap: "kept"})
	select {
	case <-got:
	case <-time.After(2 * time.Second):
		t.Fatal("the worker was never asked to leave")
	}
	card, _ := d.st.Get(worker.ID)
	if card.Status != store.StatusDone || len(pendingFrom(t, d, launcher.ID)) != 1 {
		t.Fatalf("card %s, launcher queue %d: the report must stand", card.Status, len(pendingFrom(t, d, launcher.ID)))
	}
}
