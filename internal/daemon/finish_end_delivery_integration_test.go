//go:build integration

package daemon

import (
	"net/http"
	"strings"
	"testing"

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
			stubCommits(t, true, false)
			f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })

			rec, out := finishWith(t, d, c.in)
			if rec.Code != http.StatusOK || out["launcher_told"] != true {
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
			d.drainRelays()
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

// A launcher tagged atrium:hold-notices has the ending held on its card, visible there and on the worker's work item,
// never typed, never queued, and the worker is told "held", not told.
func TestAnEndingIsHeldVisibleOnAHoldNoticesLaunchersCard(t *testing.T) {
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
			stubCommits(t, true, false)

			rec, out := finishWith(t, d, c.in)
			if rec.Code != http.StatusOK || out["launcher_told"] != "held" {
				t.Fatalf("code %d, want launcher_told held: %s", rec.Code, rec.Body)
			}
			if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
				t.Fatalf("the launcher has %d queued, want none: %v", len(msgs), msgs)
			}
			held := heldOn(t, d, launcher.ID)
			if len(held) != 1 || held[0]["source"] != NoticeReport || held[0]["about_card"] != worker.ID ||
				!strings.Contains(held[0]["text"].(string), c.want) {
				t.Fatalf("held = %v", held)
			}
			// Held is not delivered by a later nudge either: a silent stop after the ending adds nothing.
			stopSilently(t, d, "worker")
			if n := len(heldOn(t, d, launcher.ID)); n != 1 {
				t.Fatalf("%d held notices after a stop that followed the ending, want 1", n)
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
	stubCommits(t, true, false)
	if _, out := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token",
		Ended: true}); out["launcher_told"] != "held" {
		t.Fatalf("out %v", out)
	}
	got, _ := d.st.Get(launcher.ID)
	if got.Status == store.StatusDone {
		t.Fatalf("the launcher moved to %s", got.Status)
	}

	d2 := testDaemon(t)
	l2, _ := holdingPair(t, d2, OrchestratorTag)
	if _, out := finishWith(t, d2, FinishRequest{Agent: "worker", Status: ReportBlocked, Ask: "need a token",
		Ended: true}); out["launcher_told"] != true {
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
