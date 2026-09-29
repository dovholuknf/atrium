package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// r-007 stage 1b: everything for a card in a new-context cycle is held until the
// wake prompt has been typed. See newcontext.go, holdingMessages.

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
	if err := os.WriteFile(filepath.Join(dir, "HANDOFF.md"), []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	d.act.set(id, ActivityIdle, "")
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

func TestSayDuringCaptureIsHeldAndDeliveredAfterTheWake(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.md") })
	if !d.holdingMessages(id) {
		t.Fatal("not holding during capture")
	}
	out := sayViaMessage(t, d, "alice", id, heldSay)
	if out["delivered"] != "queued" || out["warning"] != newContextHoldNote {
		t.Fatalf("answer while holding: %v", out)
	}
	heldNotDelivered(t, d, id, f)

	finishCycle(t, d, id, dir, f)
	d.wake.sawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), newContextWake) })
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, heldSay) < strings.Index(got, newContextWake) {
		t.Fatalf("the held say went ahead of the wake prompt: %q", got)
	}
	if d.holdingMessages(id) {
		t.Fatal("still holding after the wake")
	}
}

func TestSayDuringClearIsHeldAndTellIsQueued(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.md") })
	finishCycle(t, d, id, dir, f)

	out, code := tell(t, d, "alice", "cycler", heldSay)
	if code != http.StatusOK || out["queued"] != true || out["typed"] == true || out["note"] != newContextHoldNote {
		t.Fatalf("tell during clear: %d %v", code, out)
	}
	heldNotDelivered(t, d, id, f)

	d.wake.sawSession(id, time.Now())
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, heldSay) < strings.Index(got, newContextWake) {
		t.Fatalf("the held say went ahead of the wake prompt: %q", got)
	}
}

func TestAFailedCycleReleasesWhatWasHeld(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.md") })
	sayViaMessage(t, d, "alice", id, heldSay)
	if strings.Contains(f.written(), heldSay) {
		t.Fatal("typed during capture")
	}
	// The capture prompt starts no turn, so the cycle fails.
	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
	if d.holdingMessages(id) {
		t.Fatal("a failed cycle is still holding")
	}
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
}

func TestDismissingACycleReleases(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.md") })
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
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if d.holdingMessages(id) {
		t.Fatal("holding with no cycle")
	}
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.md") })
	finishCycle(t, d, id, dir, f)
	d.wake.sawSession(id, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), newContextWake) })
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
}
