package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-comms-one-instruction. A worker hears one instruction for the end of a turn,
// atrium_say its launcher, and a launcher's fyi asks for no reply, so the worker
// owes none. See docs/changes/r-comms-one-instruction.md.

func TestTheNudgeNamesAtriumSayAndNotAtriumReport(t *testing.T) {
	for _, want := range []string{"atrium_say", "done <sha>", "blocked: <one line>"} {
		if !strings.Contains(silentNudgeText, want) {
			t.Errorf("the nudge misses %q: %q", want, silentNudgeText)
		}
	}
	if strings.Contains(silentNudgeText, "atrium_report") {
		t.Errorf("the nudge names atrium_report: %q", silentNudgeText)
	}
	if strings.Contains(leanSystemPrompt, "atrium_report") {
		t.Errorf("the lean worker prompt names atrium_report")
	}
}

// nudged reports whether atrium queued its silent-stop nudge for the card.
func nudged(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	for _, m := range pendingFrom(t, d, id) {
		if m.Text == silentNudgeText {
			return true
		}
	}
	return false
}

// THE t-ci-why CASE. The worker reported done, the launcher said "stop, nothing
// else to do" as an fyi, and the worker ended its turn as told. No debt, no
// nudge, no notice, on the queued path, the tell path and the typed record.
func TestALaunchersFyiAfterADoneReportOwesNothing(t *testing.T) {
	for _, door := range []string{"say", "tell", "typed"} {
		d, launcher, worker := reportedWorker(t)
		switch door {
		case "say":
			if rec := sayKind(t, d, worker, launcher.WireName, "fyi"); rec.Code >= 400 {
				t.Fatalf("say: %d %s", rec.Code, rec.Body)
			}
		case "tell":
			if rec := tellKind(t, d, worker, launcher.WireName, "fyi"); rec.Code >= 400 {
				t.Fatalf("tell: %d %s", rec.Code, rec.Body)
			}
		case "typed":
			d.notePeerTyped(worker.ID, launcher.WireName, "stop, nothing else to do", store.PromptFYI, "typed and sent")
		}
		if got, _ := d.st.Get(worker.ID); got.OwesReport() {
			t.Fatalf("%s: a launcher's fyi made the worker owe a report", door)
		}
		// Delivered, as a hook would, so the Stop below is the silent one.
		if _, err := d.takeMessages(worker.ID, "stop"); err != nil {
			t.Fatal(err)
		}
		stopTurn(t, d, "worker")
		if nudged(t, d, worker.ID) {
			t.Fatalf("%s: the worker was nudged after a launcher's fyi", door)
		}
		stopTwice(t, d, "worker")
		if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
			t.Fatalf("%s: the launcher was told of a silent stop: %v", door, msgs)
		}
	}
}

// A launcher's needs, the default, still makes the worker owe, and its silence
// still earns the nudge.
func TestALaunchersNeedsStillOwesAReport(t *testing.T) {
	for _, kind := range []string{"needs", ""} {
		d, _, worker := reportedWorker(t)
		if rec := sayKind(t, d, worker, "orchestrator", kind); rec.Code >= 400 {
			t.Fatalf("%q: %d %s", kind, rec.Code, rec.Body)
		}
		if got, _ := d.st.Get(worker.ID); !got.OwesReport() {
			t.Fatalf("%q: a launcher's message did not make the worker owe a report", kind)
		}
		if _, err := d.takeMessages(worker.ID, "stop"); err != nil {
			t.Fatal(err)
		}
		stopTurn(t, d, "worker")
		if !nudged(t, d, worker.ID) {
			t.Fatalf("%q: no nudge for a silent stop after the launcher's message", kind)
		}
	}
}

// An fyi that asks for a reply wants one, so it owes like needs.
func TestAnFyiThatAsksForAReplyStillOwes(t *testing.T) {
	if got := promptKind("fyi", true); got != "" {
		t.Fatalf("promptKind(fyi, reply) = %q, want empty", got)
	}
	if got := promptKind(" FYI ", false); got != store.PromptFYI {
		t.Fatalf("promptKind(FYI) = %q", got)
	}
}

// A say to the worker still marks the worker reported. Unchanged, checked here
// because the nudge now points every worker at it.
func TestASayToTheLauncherStillMarksTheWorkerReported(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if got, _ := d.st.Get(worker.ID); !got.OwesReport() {
		t.Fatal("the opening prompt made nothing owed")
	}
	if rec := sayKind(t, d, launcher, worker.WireName, ""); rec.Code >= 400 {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got, _ := d.st.Get(worker.ID); got.OwesReport() {
		t.Fatal("a say to the launcher did not mark the worker reported")
	}
}

// ACROSS ROOMS the kind rides the relay, and a held say keeps it for the drain.
func TestACrossRoomFyiKeepsItsKind(t *testing.T) {
	d, f := roomDaemon(t)
	peerCard(t, d, "sa1")
	code, out := say(t, d, map[string]string{"from": "sa1", "to": "w@claude-sg4", "text": "stop", "kind": "fyi"})
	if code >= 400 {
		t.Fatalf("%d %+v", code, out)
	}
	if got := f.says(); len(got) != 1 || got[0].Kind != store.PromptFYI {
		t.Fatalf("relay got %+v, want kind fyi", got)
	}

	f.set(func(RelaySay) (RelayResult, error) { return RelayResult{}, ErrRelayDown })
	if _, out := say(t, d, map[string]string{"from": "sa1", "to": "w@claude-sg4", "text": "later", "kind": "fyi"}); out["delivered"] != "held" {
		t.Fatalf("%+v", out)
	}
	settle(d)
	if rows := owed(t, d); len(rows) != 1 || rows[0].Kind != store.PromptFYI {
		t.Fatalf("outbox = %+v, want the fyi held with its kind", rows)
	}
	f.set(nil)
	d.drainRelays()
	got := f.says()
	if last := got[len(got)-1]; last.Text != "later" || last.Kind != store.PromptFYI {
		t.Fatalf("drain sent %+v, want kind fyi", last)
	}

	// A needs say goes with no kind.
	if _, out := say(t, d, map[string]string{"from": "sa1", "to": "w@claude-sg4", "text": "go"}); out["delivered"] == "held" {
		t.Fatalf("%+v", out)
	}
	got = f.says()
	if last := got[len(got)-1]; last.Kind != "" {
		t.Fatalf("a needs say went with kind %q", last.Kind)
	}
}
