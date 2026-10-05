package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Step 1 of docs/rnd/turn-end-spike.md: the Stop that ends a turn a Stop hook
// continued now reaches the room, flagged, and the room treats it as a turn
// that really is over.

func flaggedStop(t *testing.T, d *Daemon, agent string) string {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"agent": agent, "stop_hook_active": true})
	rec := httptest.NewRecorder()
	d.handleStop(rec, httptest.NewRequest(http.MethodPost, "/stop", bytes.NewReader(raw)))
	return strings.TrimSpace(rec.Body.String())
}

// The card clint saw: a message delivered at turn end sets the card running,
// the worker acts on it, and its next Stop is flagged. That Stop moves the card
// to needs-input, notes the turn for seen, nudges the worker about the silent
// stop rather than telling the launcher, never blocks, and leaves anything
// queued since then queued, ahead of the nudge.
func TestAFlaggedStopEndsTheTurnAndNeverBlocks(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	before := time.Now().Add(-time.Second)

	if _, err := d.st.QueueFromPeer(worker.ID, "land it next", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	if got := flaggedStop(t, d, "worker"); got != "{}" {
		t.Fatalf("a flagged Stop answered %s, want nothing: the hook will not block on it", got)
	}

	got, err := d.st.Get(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != store.StatusNeedsInput {
		t.Fatalf("the card is %s after the turn ended, want %s", got.Status, store.StatusNeedsInput)
	}
	if msgs := pendingFrom(t, d, worker.ID); len(msgs) != 2 || msgs[0].Text != "land it next" ||
		msgs[1].Text != silentNudgeText {
		t.Fatalf("the worker's queue holds %v, want the message still queued, then the nudge", msgs)
	}
	seen, err := d.st.GetSeen(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if seen == nil || seen.TurnEndedAt == nil || seen.TurnEndedAt.Before(before) {
		t.Fatalf("the turn was not noted for seen: %+v", seen)
	}
	if msgs := pendingFrom(t, d, launcher.ID); len(msgs) != 0 {
		t.Fatalf("the launcher has %v, want nothing until the nudge goes unanswered", msgs)
	}
}

// A worker that reported owes nothing, so its flagged Stop tells nobody.
func TestAFlaggedStopAfterAReportTellsNobody(t *testing.T) {
	d := testDaemon(t)
	launcher, _ := launchedPair(t, d)
	if rec, _ := finishWith(t, d, FinishRequest{Agent: "worker", Status: ReportDone,
		NoCommit: "research only", Recap: "done"}); rec.Code != http.StatusOK {
		t.Fatalf("report answered %d: %s", rec.Code, rec.Body)
	}
	flaggedStop(t, d, "worker")
	for _, m := range pendingFrom(t, d, launcher.ID) {
		if strings.Contains(m.Text, "without reporting") {
			t.Fatalf("a silent-stop notice after a report: %q", m.Text)
		}
	}
}
