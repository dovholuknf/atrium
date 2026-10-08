package daemon

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The after-restart wake. See docs/runtime/restart-wake.md.

// queuedWake queues a wake on a fresh card with a terminal and returns both.
func queuedWake(t *testing.T, d *Daemon, text string) (*store.RestartWake, *runner, *fakePTY) {
	t.Helper()
	target, r, f := peerPair(t, d)
	w, _, err := d.queueWake(target.ID, text, "orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	return w, r, f
}

// wakeEvents is the card's restart-wake history, as kind/what pairs.
func wakeEvents(t *testing.T, d *Daemon, taskID string) []string {
	t.Helper()
	evs, err := d.st.Events(taskID, 100)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range evs {
		var p map[string]any
		_ = json.Unmarshal(e.Payload, &p)
		switch {
		case e.Kind == store.EventNotified && p["by"] == store.RestartWakeBy:
			out = append(out, "notified:"+p["what"].(string))
		case e.Kind == store.EventPrompted && p["from"] == store.RestartWakeBy:
			out = append(out, "prompted")
		}
	}
	return out
}

// The daemon that comes back after the restart finds the wake the old one took.
func TestAWakeSurvivesTheDaemonComingBack(t *testing.T) {
	dir := t.TempDir()
	open := func() *Daemon {
		d, err := New(Options{
			AgentAddr: freePort(t), HumanAddr: freePort(t),
			DBPath:   filepath.ToSlash(filepath.Join(dir, "atrium.db")),
			LongPoll: time.Second, LocationFile: filepath.Join(dir, "daemon.json"),
		})
		if err != nil {
			t.Fatal(err)
		}
		closeAtCleanup(t, d)
		return d
	}
	d := open()
	card := cardFor(t, d, "orchestrator")
	if _, _, err := d.queueWake(card.ID, "we up", "orchestrator"); err != nil {
		t.Fatal(err)
	}
	d.Close()

	d = open()
	got := d.wake.get(card.ID)
	if got == nil || got.Text != "we up" {
		t.Fatalf("the new daemon did not load the wake: %+v", got)
	}
	v, _ := d.wakeFor(card.ID).(map[string]any)
	if v == nil || v["state"] != "waiting" {
		t.Fatalf("the card does not show the waiting wake: %+v", v)
	}
}

// Delivery waits for the runner that came back, not the one that queued it, then
// for its session to settle, the turn to end and the line to be empty. Then it
// is typed once and the wake is gone.
func TestAWakeWaitsForTheNewRunnerAnEmptyLineAndTheEndOfTheTurn(t *testing.T) {
	d := testDaemon(t)
	t.Cleanup(func() { d.pending.stopAll() })
	w, r, f := queuedWake(t, d, "we up")
	id := w.TaskID

	// The runner that is up is the old one, started before the wake.
	r.started = w.QueuedAt.Add(-time.Hour)
	d.wakeTick(w.QueuedAt.Add(time.Minute))
	if f.written() != "" {
		t.Fatalf("typed into the runner that queued the wake: %q", f.written())
	}

	// The restart brings a new runner. Its session has started but not settled.
	r.started = w.QueuedAt.Add(time.Second)
	started := r.started.Add(time.Second)
	d.wake.sawSession(id, started)
	d.wakeTick(started.Add(wakeSettle / 2))
	if f.written() != "" {
		t.Fatalf("typed before the session settled: %q", f.written())
	}

	settled := started.Add(wakeSettle)
	// Mid-turn: held.
	d.act.set(id, ActivityTool, "Bash")
	d.wakeTick(settled)
	if f.written() != "" {
		t.Fatalf("typed into a runner mid-turn: %q", f.written())
	}

	// The turn ends, but the operator has text in the line: held.
	d.act.set(id, ActivityIdle, "")
	partial := "half a thou"
	r.noteOperatorTyped([]byte(partial))
	d.wakeTick(settled)
	if f.written() != "" {
		t.Fatalf("typed into a part written line: %q", f.written())
	}

	// The line is cleared and the keyboard goes quiet: typed and sent.
	r.noteOperatorTyped(bytes.Repeat([]byte{0x7f}, len(partial)))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()
	d.wakeTick(settled)
	got := f.written()
	if !strings.Contains(got, "we up") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("the wake was not typed and sent: %q", got)
	}

	// Once. The row and the mirror are both gone, and another tick types nothing.
	if d.wake.get(id) != nil {
		t.Fatal("the wake is still in the mirror after landing")
	}
	if ws, _ := d.st.RestartWakes(); len(ws) != 0 {
		t.Fatalf("the wake is still in the store after landing: %+v", ws)
	}
	d.wakeTick(settled.Add(time.Minute))
	if again := f.written(); again != got {
		t.Fatalf("the wake was typed twice: %q", again)
	}
	evs := wakeEvents(t, d, id)
	if strings.Join(evs, ",") != "notified:queued,prompted" {
		t.Fatalf("the card's history is %v", evs)
	}
}

// A runner that never posts SessionStart is typed into once it has been up long
// enough, since there is no other signal that it is ready.
func TestAWakeReachesARunnerWithNoSessionHook(t *testing.T) {
	d := testDaemon(t)
	w, r, f := queuedWake(t, d, "carry on")
	r.started = w.QueuedAt.Add(time.Second)

	d.wakeTick(r.started.Add(wakeNoHook / 2))
	if f.written() != "" {
		t.Fatalf("typed into a runner with no hook before it had been up long: %q", f.written())
	}
	d.wakeTick(r.started.Add(wakeNoHook))
	if !strings.Contains(f.written(), "carry on") {
		t.Fatalf("never typed into a runner with no hook: %q", f.written())
	}
}

// No expiry: a card that takes days to come back still gets its wake, and the
// days in between leave it queued on the card.
func TestAWakeQueuedLongAgoIsStillDelivered(t *testing.T) {
	d := testDaemon(t)
	w, r, f := queuedWake(t, d, "we up")
	id := w.TaskID
	// The runner that queued it stays the one that is up for three days.
	r.started = w.QueuedAt.Add(-time.Hour)
	for _, later := range []time.Duration{time.Hour, 25 * time.Hour, 72 * time.Hour} {
		d.wakeTick(w.QueuedAt.Add(later))
	}
	if f.written() != "" {
		t.Fatalf("typed into the runner that queued the wake: %q", f.written())
	}
	if v, _ := d.wakeFor(id).(map[string]any); v == nil || v["text"] != "we up" {
		t.Fatalf("the wake left the card while it waited: %+v", v)
	}
	if ws, _ := d.st.RestartWakes(); len(ws) != 1 {
		t.Fatalf("the wake left the store while it waited: %+v", ws)
	}

	// The runner finally comes back.
	r.started = w.QueuedAt.Add(72 * time.Hour)
	d.wakeTick(r.started.Add(wakeNoHook))
	if !strings.Contains(f.written(), "we up") {
		t.Fatalf("a wake queued three days ago was not typed: %q", f.written())
	}
	if evs := wakeEvents(t, d, id); strings.Join(evs, ",") != "notified:queued,prompted" {
		t.Fatalf("the card's history is %v", evs)
	}
}

// The wake is typed behind the same grey `[atrium] ...` label a peer's message
// gets, so nobody reads it as the operator, and the prompt it starts is not read
// as the operator either.
func TestAWakeIsTypedBehindTheAtriumLabel(t *testing.T) {
	d := testDaemon(t)
	w, r, f := queuedWake(t, d, "we up")
	r.started = w.QueuedAt.Add(time.Second)
	d.wakeTick(r.started.Add(wakeNoHook))

	got := f.written()
	want := "\x1b[38;5;244m[atrium] restart wake: \x1b[0m"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("the wake was not typed behind the label: %q", got)
	}
	if wakeLabel != atriumLabel("restart wake:") || peerBanner("x") != atriumLabel("x says:") {
		t.Fatal("the wake and the peer banner do not share one label style")
	}
	if strings.ContainsAny(wakeLabel, "\r\n") {
		t.Fatalf("the label can press Enter: %q", wakeLabel)
	}
	if !strings.Contains(got[len(want):], "we up") {
		t.Fatalf("the text does not follow the label: %q", got)
	}
	if !r.promptWasPeer(time.Now()) {
		t.Fatal("the prompt the wake started is read as the operator")
	}
}

// The endpoint a deploy script calls: by wire name, one per card, and cleared.
func TestTheRestartWakeEndpoint(t *testing.T) {
	d := testDaemon(t)
	card := cardFor(t, d, "orchestrator")
	call := func(method, who, body string) (*httptest.ResponseRecorder, map[string]any) {
		req := httptest.NewRequest(method, "/v1/tasks/"+who+"/restart-wake", strings.NewReader(body))
		req.SetPathValue("id", who)
		rec := httptest.NewRecorder()
		d.handleRestartWake(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec, out
	}

	if rec, _ := call(http.MethodPost, card.WireName, `{"text":"first","by":"deploy.ps1"}`); rec.Code != 200 {
		t.Fatalf("queue by wire name answered %d: %s", rec.Code, rec.Body)
	}
	rec, out := call(http.MethodPost, card.ID, `{"text":"second"}`)
	if rec.Code != 200 || out["replaced"] != "first" {
		t.Fatalf("a second wake did not replace the first: %d %v", rec.Code, out)
	}
	if _, out := call(http.MethodGet, card.ID, ""); out["wake"].(map[string]any)["text"] != "second" {
		t.Fatalf("reading the wake answered %v", out)
	}
	if rec, _ := call(http.MethodPost, card.ID, `{"text":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("an empty wake answered %d", rec.Code)
	}
	if rec, _ := call(http.MethodPost, "nobody", `{"text":"x"}`); rec.Code != http.StatusNotFound {
		t.Fatalf("an unknown card answered %d", rec.Code)
	}
	if _, out := call(http.MethodDelete, card.ID, ""); out["cleared"] != true {
		t.Fatalf("clearing answered %v", out)
	}
	if d.wakeFor(card.ID) != nil {
		t.Fatal("the cleared wake is still on the card")
	}
}
