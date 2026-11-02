//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// Typing is held for the whole cycle, every waiting step included.
func TestTypingIsHeldForTheWholeCycle(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	for i := 0; i < 5; i++ {
		if !d.Nctx.TypingHeld(id) {
			t.Fatal("typing not held while waiting for atrium ready")
		}
		time.Sleep(10 * time.Millisecond) // wait: a negative check, typing must stay held while atrium ready is awaited
	}
	gen := d.Nctx.Get(id).Gen
	d.Nctx.SetTyping(id, gen, true)
	if !d.Nctx.TypingHeld(id) {
		t.Fatal("typing not held while atrium types")
	}
	if v, _ := d.NewContextFor(id).(map[string]any); v == nil || v["typing"] != true {
		t.Fatalf("the card does not say atrium is typing: %v", d.NewContextFor(id))
	}
	d.Nctx.SetTyping(id, gen, false)

	// The clear and wake steps waiting on the card or the new session.
	finishCycle(t, d, id, dir, f)
	until(t, "the clear step", func() bool {
		v, _ := d.NewContextFor(id).(map[string]any)
		return v != nil && v["step"] == cycle.NewContextClear
	})
	if !d.Nctx.TypingHeld(id) {
		t.Fatal("typing not held while waiting for the new session")
	}
	if v, _ := d.NewContextFor(id).(map[string]any); v == nil || v["held"] != true {
		t.Fatalf("the card does not say typing is held: %v", d.NewContextFor(id))
	}
	if d.Nctx.SetTyping(id, gen+99, true) {
		t.Fatal("another run's write set the flag")
	}
}

// The way out: a dismissal gives the terminal back at once, even with a write
// marked under way, and sends what was queued.
func TestDismissingGivesTheTerminalBackAndSendsTheQueue(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	peerCard(t, d, "alice")
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.Nctx.SetTyping(id, d.Nctx.Get(id).Gen, true)
	sayViaMessage(t, d, "alice", id, "FIRSTQUEUED")
	if strings.Contains(f.written(), "FIRSTQUEUED") {
		t.Fatal("typed during the cycle")
	}

	r := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.HandleNewContext(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dismiss answered %d", w.Code)
	}
	if d.Nctx.TypingHeld(id) || d.HoldingMessages(id) {
		t.Fatal("still held after the dismissal")
	}
	until(t, "the queued text", func() bool { return strings.Contains(f.written(), "FIRSTQUEUED") })
}

// Several queued messages go in the order they were queued, after the wake prompt.
func TestQueuedTextGoesInOrderAfterTheWake(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, dir := ncCard(t, d)
	peerCard(t, d, "alice")
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	sayViaMessage(t, d, "alice", id, "QUEUEDONE")
	time.Sleep(5 * time.Millisecond) // wait: spaces the two queued stamps so their order is the one under test
	sayViaMessage(t, d, "alice", id, "QUEUEDTWO")
	finishCycle(t, d, id, dir, f)
	d.Wake.SawSession(id, time.Now())
	until(t, "both messages", func() bool {
		w := f.written()
		return strings.Contains(w, "QUEUEDONE") && strings.Contains(w, "QUEUEDTWO")
	})
	got := f.written()
	wake, one, two := strings.Index(got, ncWakeMark), strings.Index(got, "QUEUEDONE"), strings.Index(got, "QUEUEDTWO")
	if !(wake < one && one < two) {
		t.Fatalf("out of order, wake %d one %d two %d: %q", wake, one, two, got)
	}
}
