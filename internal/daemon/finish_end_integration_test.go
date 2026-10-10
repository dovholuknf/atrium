//go:build integration

package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func stubCommits(t *testing.T, inDir, onHub bool) {
	t.Helper()
	oldDir, oldHub := commitInDir, commitOnHub
	commitInDir = func(string, string) bool { return inDir }
	commitOnHub = func(*Daemon, *store.Task, string) bool { return onHub }
	t.Cleanup(func() { commitInDir, commitOnHub = oldDir, oldHub })
}

func TestAnEndingDoneWithAnUnknownCommitIsRefusedAndRecordsNothing(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	stubCommits(t, false, false)
	rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true})
	if rec.Code != 400 || !strings.Contains(rec.Body.String(), "abc1234") {
		t.Fatalf("code %d, body %s", rec.Code, rec.Body)
	}
	got, _ := d.st.Get(worker.ID)
	if got.ReportedAt != nil || got.Status == "done" {
		t.Fatalf("a refused ending was recorded: %+v", got)
	}
}

func TestAnEndingDoneWithABadShapeIsRefused(t *testing.T) {
	d := testDaemon(t)
	launchedPair(t, d)
	stubCommits(t, true, true)
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "nothex!", Ended: true}); rec.Code != 400 {
		t.Fatalf("code %d", rec.Code)
	}
}

func TestAnEndingDoneTakesACommitTheHubHasAndTellsTheLauncherWithNoNudge(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stubCommits(t, false, true)
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234",
		Recap: "done abc1234", Ended: true})
	if rec.Code != 200 || out["launcher_told"] != true {
		t.Fatalf("code %d, out %v", rec.Code, out)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "done at abc1234") {
		t.Fatalf("the launcher has %+v", msgs)
	}
	stopTwice(t, d, "worker")
	if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
		t.Fatalf("the worker was nudged after done: %d", n)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher heard %d, want only the report", n)
	}
}

func TestAnEndingBlockedRefusesNoReasonAndOver50WordsAndDeliversTheReasonWithNoNudge(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	for _, why := range []string{"", " \n ", strings.Repeat("w ", 51)} {
		if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: why, Ended: true}); rec.Code != 400 {
			t.Fatalf("%q: code %d", why, rec.Code)
		}
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a refused ending told the launcher: %d", n)
	}
	reason := "need a token\nfrom the operator"
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: reason, Ended: true}); rec.Code != 200 {
		t.Fatalf("code %d, %s", rec.Code, rec.Body)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "blocked") || !strings.Contains(msgs[0].Text, "from the operator") {
		t.Fatalf("the launcher has %+v", msgs)
	}
	stopTwice(t, d, "worker")
	if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
		t.Fatalf("the worker was nudged after blocked: %d", n)
	}
	w, err := d.st.WorkItem(worker.ID)
	if err == nil && w.State != "reported" {
		t.Fatalf("the work item is %q, want reported", w.State)
	}
}
