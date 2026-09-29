package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// stopTurnWithBackground is the Stop hook for a turn that ended with `n` shells
// or headless runs still going.
func stopTurnWithBackground(t *testing.T, d *Daemon, agent string, n int) {
	t.Helper()
	raw, _ := json.Marshal(map[string]any{"agent": agent, "background_running": n})
	rec := httptest.NewRecorder()
	d.handleStop(rec, httptest.NewRequest(http.MethodPost, "/stop", bytes.NewReader(raw)))
}

// sa31. A worker whose turn ended with background runs going is waiting, not
// stopped: neither the launcher notice nor the board escalation fires.
func TestATurnEndedOnBackgroundWorkIsNotASilentStop(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stopTurnWithBackground(t, d, "worker", 5)
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the launcher was told of a silent stop with 5 runs going: %d", n)
	}
	for _, after := range []time.Duration{3 * time.Minute, time.Hour} {
		if err := d.watchWorkers(time.Now().Add(after)); err != nil {
			t.Fatal(err)
		}
		if x := d.esc.get(worker.ID); x != nil {
			t.Fatalf("stuck %s after a stop with background work: %+v", after, x)
		}
	}
	if n := len(pendingFrom(t, d, launcher.ID)); n != 0 {
		t.Fatalf("the watchdog told the launcher while work was running: %d", n)
	}
}

// When the work ends the session wakes and stops again, and that Stop names
// nothing running: the clock is that turn's end, not the first one's.
func TestBackgroundWorkEndingStartsTheSilentStopClock(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	stopTurnWithBackground(t, d, "worker", 2)
	time.Sleep(5 * time.Millisecond)
	stopTurnWithBackground(t, d, "worker", 0)
	if n := len(pendingFrom(t, d, launcher.ID)); n != 1 {
		t.Fatalf("the launcher has %d notices after the work ended, want one", n)
	}
	ended, err := d.st.TurnEndedAt(worker.ID)
	if err != nil || ended == nil {
		t.Fatalf("no turn end: %v", err)
	}
	if err := d.watchWorkers(ended.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	x := d.esc.get(worker.ID)
	if x == nil || x.Source != NoticeSilentStop || !x.Since.Equal(*ended) {
		t.Fatalf("escalation %+v, want a silent stop counting from %s", x, ended)
	}
}

// A dev server left running names itself in every Stop, so the hold is bounded.
func TestBackgroundWorkHoldsTheAlertOnlyForAWhile(t *testing.T) {
	d := testDaemon(t)
	_, worker := launchedPair(t, d)
	stopTurnWithBackground(t, d, "worker", 1)
	if err := d.watchWorkers(time.Now().Add(time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(worker.ID); x != nil {
		t.Fatalf("held card escalated: %+v", x)
	}
	base := d.act.now
	d.act.now = func() time.Time { return base().Add(BackgroundHoldMax + time.Minute) }
	if err := d.watchWorkers(time.Now().Add(BackgroundHoldMax + time.Minute)); err != nil {
		t.Fatal(err)
	}
	if x := d.esc.get(worker.ID); x == nil || x.Source != NoticeSilentStop {
		t.Fatalf("escalation %+v, want a silent stop once the hold ran out", x)
	}
}
