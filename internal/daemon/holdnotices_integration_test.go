//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// heldOn is the held notices on a card, oldest first.
func heldOn(t *testing.T, d *Daemon, id string) []map[string]any {
	t.Helper()
	evs, err := d.st.Events(id, 0)
	if err != nil {
		t.Fatal(err)
	}
	var out []map[string]any
	for _, e := range evs {
		if e.Kind != store.EventNotified {
			continue
		}
		var p map[string]any
		if json.Unmarshal(e.Payload, &p) == nil && p["held"] == true {
			out = append(out, p)
		}
	}
	return out
}

func holdingPair(t *testing.T, d *Daemon, tag string) (launcher, worker *store.Task) {
	t.Helper()
	launcher, worker = launchedPair(t, d)
	if err := d.st.SetTags(launcher.ID, []string{tag}); err != nil {
		t.Fatal(err)
	}
	worker, err := d.st.Get(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if made, err := d.st.CreateWorkItem(worker, store.NewWorkItem{Brief: "hold test"}); err != nil || !made {
		t.Fatalf("work item: %v %v", made, err)
	}
	return launcher, worker
}

func TestTheOrchestratorGetsASilentStopOnItsCardNotInItsTerminal(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)

	stopTwice(t, d, "worker")
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
		t.Fatalf("the orchestrator has %d queued, want none: %v", len(msgs), msgs)
	}
	held := heldOn(t, d, launcher.ID)
	if len(held) != 1 {
		t.Fatalf("%d held notices for one stop, want one: %v", len(held), held)
	}
	if held[0]["source"] != NoticeSilentStop || held[0]["about_card"] != worker.ID ||
		!strings.Contains(held[0]["text"].(string), "without reporting") {
		t.Fatalf("held notice = %v", held[0])
	}
	log, err := d.st.WorkLog(worker.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range log {
		found = found || strings.Contains(e.Text, "silent-stop notice held for "+launcher.WireName)
	}
	if !found {
		t.Fatalf("the worker's work item does not say the notice was held: %v", log)
	}
}

func TestHoldNoticesTagHoldsNoticesAndReports(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, HoldNoticesTag)

	stopTwice(t, d, "worker")
	if n := len(heldOn(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d held notices, want one", n)
	}
	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone,
		NoCommit: "research only", Recap: "the matrix is written up"})
	if rec.Code != http.StatusOK || out["launcher_told"] != true {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
		t.Fatalf("the launcher has %d queued, want none: %v", len(msgs), msgs)
	}
	held := heldOn(t, d, launcher.ID)
	if len(held) != 2 || held[1]["source"] != NoticeReport || held[1]["about_card"] != worker.ID ||
		!strings.Contains(held[1]["text"].(string), "the matrix is written up") {
		t.Fatalf("held = %v, want the silent stop then the report", held)
	}
}

func TestTheOrchestratorTagAloneStillHasReportsQueued(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, OrchestratorTag)

	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone,
		NoCommit: "research only", Recap: "the matrix is written up"})
	if rec.Code != http.StatusOK || out["launcher_told"] != true {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}
	msgs := pendingFrom(t, d, launcher.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "the matrix is written up") {
		t.Fatalf("the launcher has %d queued, want the report: %v", len(msgs), msgs)
	}
}

func TestAnEndedNoticeIsHeldForTheOrchestrator(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := holdingPair(t, d, OrchestratorTag)

	if err := d.st.AppendEvent(worker.ID, store.EventExited, map[string]any{"by": "supervisor", "exit_code": 0}); err != nil {
		t.Fatal(err)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
		t.Fatalf("the orchestrator has %d queued, want none: %v", len(msgs), msgs)
	}
	held := heldOn(t, d, launcher.ID)
	if len(held) != 1 || held[0]["source"] != store.NoticeEnded ||
		!strings.Contains(held[0]["text"].(string), "ended without a final report") {
		t.Fatalf("held notices = %v", held)
	}
}

func TestAnUntaggedLauncherStillHasItsNoticeQueued(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, "orchestrators")

	stopTwice(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued, want the silent stop", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held for a launcher that does not hold them", n)
	}
}
