//go:build integration

package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const heldSay = "PEERSAYHELD"

// sayViaMessage posts a session's say to the message endpoint.
func sayViaMessage(t *testing.T, d *Daemon, from, id, text string) map[string]any {
	t.Helper()
	body := `{"from":"` + from + `","text":"` + text + `"}`
	r := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+id+"/message", strings.NewReader(body))
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleMessage(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("message answered %d: %s", w.Code, w.Body.String())
	}
	out := map[string]any{}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return out
}

// heldNotDelivered checks a say is queued, unread by either hook and untyped.
func heldNotDelivered(t *testing.T, d *Daemon, id string, f *fakePTY) {
	t.Helper()
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("a say was typed during the cycle: %q", f.written())
	}
	for _, via := range []string{"permission", "stop"} {
		if msgs, _ := d.takeMessages(id, via); len(msgs) != 0 {
			t.Fatalf("the %s hook took a held message", via)
		}
	}
	if n := len(pendingFrom(t, d, id)); n != 1 {
		t.Fatalf("%d messages queued, want the held one", n)
	}
}

func finishCycle(t *testing.T, d *Daemon, id, dir string, f *fakePTY) {
	t.Helper()
	d.act.set(id, ActivityThinking, "")
	task, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	ncAck(t, d, task)
	time.Sleep(60 * time.Millisecond)
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

// ncWakeMark is the part of the wake prompt that does not depend on the card's handoff path.
const ncWakeMark = "and continue."

func TestSayDuringCaptureIsHeldAndDeliveredAfterTheWake(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if !d.holdingMessages(id) {
		t.Fatal("not holding while waiting for atrium ready")
	}
	out := sayViaMessage(t, d, "alice", id, heldSay)
	if out["delivered"] != "queued" || out["warning"] != newContextHoldNote {
		t.Fatalf("answer while holding: %v", out)
	}
	heldNotDelivered(t, d, id, f)

	finishCycle(t, d, id, dir, f)
	d.wake.sawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, heldSay) < strings.Index(got, ncWakeMark) {
		t.Fatalf("the held say went ahead of the wake prompt: %q", got)
	}
	if d.holdingMessages(id) {
		t.Fatal("still holding after the wake")
	}
}

func TestSayDuringClearIsHeldAndTellIsQueued(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	finishCycle(t, d, id, dir, f)

	out, code := tell(t, d, "alice", "cycler", heldSay)
	if code != http.StatusOK || out["queued"] != true || out["typed"] == true || out["note"] != newContextHoldNote {
		t.Fatalf("tell during clear: %d %v", code, out)
	}
	heldNotDelivered(t, d, id, f)

	d.wake.sawSession(id, time.Now())
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, heldSay) < strings.Index(got, ncWakeMark) {
		t.Fatalf("the held say went ahead of the wake prompt: %q", got)
	}
}

func TestAFailedCycleReleasesWhatWasHeld(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	sayViaMessage(t, d, "alice", id, heldSay)
	if strings.Contains(f.written(), heldSay) {
		t.Fatal("typed while waiting for atrium ready")
	}
	// No new session starts after /clear, so the cycle fails.
	finishCycle(t, d, id, dir, f)
	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
	if d.holdingMessages(id) {
		t.Fatal("a failed cycle is still holding")
	}
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
}

func TestDismissingACycleReleases(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.act.set(id, ActivityThinking, "")
	sayViaMessage(t, d, "alice", id, heldSay)
	r := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	r.SetPathValue("id", id)
	d.handleNewContext(httptest.NewRecorder(), r)
	if d.holdingMessages(id) {
		t.Fatal("still holding after a dismissal")
	}
}

// The cycle's own typing is not held, and a card outside a cycle is unaffected.
func TestHoldDoesNotStopTheCyclesOwnTyping(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if d.holdingMessages(id) {
		t.Fatal("holding with no cycle")
	}
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	finishCycle(t, d, id, dir, f)
	d.wake.sawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
}

func TestOperatorSayDuringACycleIsNotHeldButAPeersIs(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if out := sayViaMessage(t, d, "alice", id, heldSay); out["warning"] != newContextHoldNote {
		t.Fatalf("a peer's say was not held: %v", out)
	}
	out := sayViaMessage(t, d, "", id, "OPERATORLINE")
	if out["warning"] == newContextHoldNote || out["delivered"] == "queued" {
		t.Fatalf("the operator's say was held by the cycle: %v", out)
	}
	until(t, "the operator's line", func() bool { return strings.Contains(f.written(), "OPERATORLINE") })
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("the peer's say was typed during the cycle: %q", f.written())
	}
}

func TestACycleWhoseLimitPromptWasNotTypedHoldsNothing(t *testing.T) {
	d := testDaemon(t)
	id := "cycler-untyped"
	d.nctx.mu.Lock()
	d.nctx.by[id] = &newContext{step: NewContextLimit, since: time.Now(), armed: true}
	d.nctx.mu.Unlock()
	if d.holdingMessages(id) || d.holdingFrom(id, "alice") {
		t.Fatal("a cycle that has typed nothing held messages")
	}
	d.nctx.mu.Lock()
	d.nctx.by[id].prompted = 1
	d.nctx.mu.Unlock()
	if !d.holdingMessages(id) || !d.holdingFrom(id, "alice") || d.holdingFrom(id, "") {
		t.Fatal("after the prompt the cycle should hold a peer's message and never the operator's")
	}
}

func TestABoardCycleWaitsForThePartTypedLineBeforeItHolds(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID
	run := d.sup.get(id)
	run.noteOperatorTyped([]byte("sure. you can move them"))

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	time.Sleep(100 * time.Millisecond) // wait: negative check, the cycle must not hold
	v, _ := d.newContextFor(id).(map[string]any)
	if d.nctx.typingHeld(id) || d.holdingMessages(id) || v["held"] != false || strings.Contains(f.written(), limitPrompt) {
		t.Fatalf("the cycle held input over a part-typed line: %v", v)
	}

	run.noteOperatorTyped([]byte(strings.Repeat("\x7f", len("sure. you can move them"))))
	run.typeMu.Lock()
	run.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	run.typeMu.Unlock()
	until(t, "the hold once the line is clear", func() bool { return d.nctx.typingHeld(id) })
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if v, _ := d.newContextFor(id).(map[string]any); v["held"] != true {
		t.Fatalf("an armed cycle does not say it holds: %v", v)
	}
}
