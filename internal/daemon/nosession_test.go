package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A say to a card that finished with no process left is undeliverable and says
// to resume it first. It is not queued, and nothing is held for its line.
func TestASayToAGoneSessionIsUndeliverable(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	if err := d.st.SetStatus(target.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/tasks/"+target.ID+"/message",
		strings.NewReader(`{"text":"one more thing","from":"orchestrator"}`))
	req.SetPathValue("id", target.ID)
	d.handleMessage(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("say answered %d: %s", rec.Code, rec.Body)
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out["delivered"] != "undeliverable" {
		t.Fatalf("delivered %v, want undeliverable for a card with no session", out["delivered"])
	}
	if w, _ := out["warning"].(string); !strings.Contains(w, "resume it first") {
		t.Fatalf("the warning does not say to resume it: %q", w)
	}
	if n := len(pendingFrom(t, d, target.ID)); n != 0 {
		t.Fatalf("%d queued for a card nobody can read", n)
	}
	if a := d.act.get(target.ID); a != nil && a.HeldCount > 0 {
		t.Fatalf("a message was held for the line of a card with no session: %+v", a)
	}
}

// A card still running after it reported done is somebody to talk to, and a
// card that is not over is never gone whatever its pid says.
func TestOnlyAnOverCardWithNoProcessIsGone(t *testing.T) {
	for _, c := range []struct {
		status string
		pid    int
		gone   bool
	}{
		{store.StatusDone, 0, true},
		{store.StatusDead, 0, true},
		{store.StatusDone, os.Getpid(), false},
		{store.StatusNeedsInput, 0, false},
		{store.StatusShelved, 0, false},
	} {
		if got := sessionGone(&store.Task{Status: c.status, PID: c.pid}); got != c.gone {
			t.Errorf("%s with pid %d: gone %v, want %v", c.status, c.pid, got, c.gone)
		}
	}
}

// THE CHIP THAT CAME BACK. A message held for the line, then the session ends
// while its runner lingers. The end forgets the chip, and the next retry used
// to set `held_for: line` again, for as long as the runner stayed. It is
// dropped instead, and the message stays queued for a resumed session.
func TestAHeldMessageOnAnEndedSessionIsDropped(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a command"))

	m, err := d.st.QueueFromPeer(target.ID, "still there?", "orchestrator")
	if err != nil {
		t.Fatal(err)
	}
	d.deferPeerInjection(target.ID, m.ID, "orchestrator", "still there?", false)
	if a := d.act.get(target.ID); a == nil || a.HeldFor != HeldForLine {
		t.Fatalf("the message was not held for the line to begin with: %+v", a)
	}

	// What the SessionEnd hook leaves: done, the chip forgotten, the runner still
	// registered because its process has not gone yet.
	if err := d.st.SetStatus(target.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	d.act.forget(target.ID)
	d.pending.attempt(target.ID)

	if a := d.act.get(target.ID); a != nil && a.HeldCount > 0 {
		t.Fatalf("the held chip came back on a card whose session ended: held_for %q", a.HeldFor)
	}
	if n := heldCount(d.pending, target.ID); n != 0 {
		t.Fatalf("%d still held for a session that ended", n)
	}
	if n := len(pendingFrom(t, d, target.ID)); n != 1 {
		t.Fatalf("%d queued, want the message kept for a resumed session", n)
	}
}
