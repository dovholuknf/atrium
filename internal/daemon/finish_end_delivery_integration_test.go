//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// atrium_done and atrium_blocked from a worker whose launcher is on another room. The report is held in the relay
// outbox in the same transaction as the end, goes to the hub addressed to the launcher's CARD, and is told true.
func TestAnEndingReachesALauncherOnAnotherRoomByWayOfTheHub(t *testing.T) {
	for _, c := range []struct {
		name string
		in   FinishRequest
		want string
	}{
		{"done", FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "all green", Ended: true},
			"done abc1234"},
		{"blocked", FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token", Ended: true},
			"need a token"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d, f := roomDaemon(t)
			w := remoteWorker(t, d)
			stubCommits(t, d, true, false)
			f.set(func(hublink.RelaySay) (hublink.RelayResult, error) {
				return hublink.RelayResult{}, hublink.ErrRelayDown
			})

			rec, out := finishWith(t, d, c.in)
			if rec.Code != http.StatusOK || out["launcher_told"] != "relayed" {
				t.Fatalf("code %d, out %v", rec.Code, out)
			}
			// Held first, so a hub that is down loses nothing.
			settle(d)
			rows := owed(t, d)
			if len(rows) != 1 || rows[0].FromTask != w.ID || rows[0].ToRoom != "claude-sg4" || rows[0].ToCard != "L1" ||
				rows[0].ToName != "orch" || !strings.Contains(rows[0].Text, c.want) {
				t.Fatalf("outbox = %+v", rows)
			}
			// Then the hub answers, and the report goes once, to the card, from the worker.
			f.set(nil)
			d.DrainRelays()
			sent := f.says()
			if len(sent) == 0 {
				t.Fatal("nothing reached the hub")
			}
			last := sent[len(sent)-1]
			if last.To != "L1" || last.Room != "claude-sg4" || last.From != "worker" || !strings.Contains(last.Text, c.want) {
				t.Fatalf("the hub got %+v", last)
			}
			if len(owed(t, d)) != 0 {
				t.Fatal("the report is still owed after it went")
			}
			// No orphan notice: somebody was told.
			if n := len(pendingFrom(t, d, w.ID)); n != 0 {
				t.Fatalf("the worker was nudged after its ending: %d", n)
			}
			got, _ := d.st.Get(w.ID)
			if got.OwesReport() {
				t.Fatal("the worker ended and still owes a report")
			}
		})
	}
}

// A worker's ending always reaches its launcher as typed input at turn end, whatever tags the launcher carries, and
// the worker is told true only because it is queued for real. Never held on a card.
func TestAnEndingIsQueuedForTurnEndOnAHoldNoticesLaunchersCard(t *testing.T) {
	for _, c := range []struct {
		name string
		in   FinishRequest
		want string
	}{
		{"done", FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Recap: "all green", Ended: true},
			"done abc1234"},
		{"blocked", FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token", Ended: true},
			"need a token"},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := testDaemon(t)
			launcher, worker := holdingPair(t, d, HoldNoticesTag)
			stubCommits(t, d, true, false)

			rec, out := finishWith(t, d, c.in)
			if rec.Code != http.StatusOK || out["launcher_told"] != "queued" {
				t.Fatalf("code %d, want launcher_told queued: %s", rec.Code, rec.Body)
			}
			msgs := pendingFrom(t, d, launcher.ID)
			if len(msgs) != 1 || !msgs[0].WaitTurn || !strings.Contains(msgs[0].Text, c.want) {
				t.Fatalf("the launcher has %+v, want the ending queued for the turn end (worker %s)", msgs, worker.ID)
			}
			if n := len(heldOn(t, d, launcher.ID)); n != 0 {
				t.Fatalf("%d held, want none", n)
			}
			// A silent stop after the ending adds nothing.
			stopSilently(t, d, "worker")
			if n := len(heldOn(t, d, launcher.ID)); n != 0 {
				t.Fatalf("%d held notices after a stop that followed the ending, want 0", n)
			}
			if n := len(pendingFrom(t, d, worker.ID)); n != 0 {
				t.Fatalf("the worker was nudged after its ending: %d", n)
			}
		})
	}
}

// A parked hold-notices launcher is not woken by the held ending, and a launcher that only holds the orchestrator's
// notices has the same ending queued for it, as an ending always was.
func TestAnEndingResumesNoHoldNoticesLauncherAndQueuesForAnOrchestratorOne(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, HoldNoticesTag)
	stubCommits(t, d, true, false)
	if _, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token",
		Ended: true}); out["launcher_told"] != "queued" {
		t.Fatalf("out %v", out)
	}
	got, _ := d.st.Get(launcher.ID)
	if got.Status == store.StatusDone {
		t.Fatalf("the launcher moved to %s", got.Status)
	}

	d2 := testDaemon(t)
	l2, _ := holdingPair(t, d2, OrchestratorTag)
	if _, out := finishWith(t, d2, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token",
		Ended: true}); out["launcher_told"] != "queued" {
		t.Fatalf("out %v", out)
	}
	msgs := pendingFrom(t, d2, l2.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "need a token") {
		t.Fatalf("the orchestrator has %+v", msgs)
	}
	if n := len(heldOn(t, d2, l2.ID)); n != 0 {
		t.Fatalf("an ending was held for an orchestrator-only launcher: %d", n)
	}
}

// A launcher's stop is answered with the same done again. The launcher already has that sha, so the repeat is recorded
// and sends nothing, and the silent stop that follows finds the debt settled.
func TestARepeatDoneAfterALauncherStopIsSilent(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stubCommits(t, d, true, false)

	if rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true}); rec.Code != http.StatusOK {
		t.Fatalf("code %d: %v", rec.Code, out)
	}
	var ids []string
	for _, m := range pendingFrom(t, d, launcher.ID) {
		ids = append(ids, m.ID)
	}
	if err := d.st.MarkDelivered(launcher.ID, "terminal", ids); err != nil {
		t.Fatal(err)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d pending after draining", n)
	}

	clockMoves(t)
	prompt(t, d, worker.ID)
	if rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true}); rec.Code != http.StatusOK {
		t.Fatalf("code %d: %v", rec.Code, out)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a repeat done told the launcher: %d pending", n)
	}
	stopSilently(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a silent stop after the repeat rang the launcher: %d pending", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held on the launcher", n)
	}
}

// A resident card's done reports a task and leaves the card open, and a silent stop after a launcher prompt is not
// a failure to report.
func TestAResidentDoneNeitherExitsNorRingsOnSilence(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if err := d.st.SetTags(worker.ID, []string{"sdk", OriginAgentTag, ResidentTag}); err != nil {
		t.Fatal(err)
	}
	stubCommits(t, d, true, false)
	task, _ := d.st.Get(worker.ID)
	if d.exitsOnReport(task, FinishRequest{Status: ReportDone, SHA: "abc1234"}) {
		t.Fatal("a resident's done exits its runner")
	}
	if rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true}); rec.Code != http.StatusOK {
		t.Fatalf("code %d: %v", rec.Code, out)
	}
	var ids []string
	for _, m := range pendingFrom(t, d, launcher.ID) {
		ids = append(ids, m.ID)
	}
	if err := d.st.MarkDelivered(launcher.ID, "terminal", ids); err != nil {
		t.Fatal(err)
	}
	clockMoves(t)
	prompt(t, d, worker.ID)
	stopSilently(t, d, "worker")
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("a silent stop rang the launcher about a resident: %d pending", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held on the launcher", n)
	}
}

// A done queued for an idle orchestrator is typed into its terminal when the gate is open, and leaves its queue.
func TestADoneIsTypedIntoAnIdleOrchestratorsTerminal(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, OrchestratorTag)
	r, f := typedRunner(t, d, launcher.ID)
	stubCommits(t, d, true, false)
	if err := d.st.SetStatus(launcher.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	// Emptied and quiet past the idle window: the gate is open.
	r.noteOperatorTyped([]byte("ls\r"))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()

	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true})
	if rec.Code != http.StatusOK || out["launcher_told"] != "queued" {
		t.Fatalf("code %d, want launcher_told queued: %s", rec.Code, rec.Body)
	}
	d.pending.attempt(launcher.ID)
	got := f.written()
	if !strings.Contains(got, "done abc1234") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("the done was not typed and submitted: %q", got)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d still pending after it was typed", n)
	}
	if n := len(heldOn(t, d, launcher.ID)); n != 0 {
		t.Fatalf("%d held", n)
	}
}

// A done queued for an orchestrator with no terminal to type into is carried by its Stop hook instead, never lost. A
// terminal that holds it for the typist keeps it off the hooks on purpose, so this is the hook's own route.
func TestADoneToABusyOrchestratorIsCarriedByItsStopHook(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := holdingPair(t, d, OrchestratorTag)
	stubCommits(t, d, true, false)

	rec, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone, SHA: "abc1234", Ended: true})
	if rec.Code != http.StatusOK || out["launcher_told"] != "queued" {
		t.Fatalf("code %d, want launcher_told queued: %s", rec.Code, rec.Body)
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("%d queued for the launcher, want 1", n)
	}
	if reason := stopTurn(t, d, "orchestrator"); !strings.Contains(reason, "done abc1234") {
		t.Fatalf("the Stop hook answered %q, want it to carry the done", reason)
	}
}
