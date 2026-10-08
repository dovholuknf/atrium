package daemon

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// r-cycle-holds-typing-while-waiting: board typing is refused only while atrium is
// typing into the terminal itself, never while the cycle waits on the agent, and
// there is always a way out.

// Every waiting step lets a person type. Only atrium's own write refuses it.
func TestTypingIsHeldOnlyWhileAtriumTypes(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	// The limit step, waiting for atrium ready: nothing is being typed.
	until(t, "the write to end", func() bool { return !d.nctx.typingHeld(id) })
	for i := 0; i < 5; i++ {
		if d.nctx.typingHeld(id) {
			t.Fatal("typing held while waiting for atrium ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Atrium's own write.
	gen := d.nctx.get(id).gen
	d.nctx.setTyping(id, gen, true)
	if !d.nctx.typingHeld(id) {
		t.Fatal("typing not held while atrium types")
	}
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["typing"] != true {
		t.Fatalf("the card does not say atrium is typing: %v", d.newContextFor(id))
	}
	d.nctx.setTyping(id, gen, false)

	// The clear and wake steps waiting on the card or the new session.
	finishCycle(t, d, id, dir, f)
	until(t, "the clear to end", func() bool { return !d.nctx.typingHeld(id) })
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["step"] != NewContextClear {
		t.Fatalf("not waiting on the new session: %v", d.newContextFor(id))
	}
	if d.nctx.typingHeld(id) {
		t.Fatal("typing held while waiting for the new session")
	}
	// A stale run cannot leave the flag on for the next one.
	if d.nctx.setTyping(id, gen+99, true) || d.nctx.typingHeld(id) {
		t.Fatal("another run's write set the flag")
	}
}

// The way out: a dismissal gives the terminal back at once, even with a write
// marked under way, and sends what was queued.
func TestDismissingGivesTheTerminalBackAndSendsTheQueue(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.nctx.setTyping(id, d.nctx.get(id).gen, true)
	sayViaMessage(t, d, "", id, "FIRSTQUEUED")
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["queued"] != 1 {
		t.Fatalf("the card does not say a message is queued: %v", d.newContextFor(id))
	}
	if strings.Contains(f.written(), "FIRSTQUEUED") {
		t.Fatal("typed during the cycle")
	}

	r := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	r.SetPathValue("id", id)
	w := httptest.NewRecorder()
	d.handleNewContext(w, r)
	if w.Code != http.StatusOK {
		t.Fatalf("dismiss answered %d", w.Code)
	}
	if d.nctx.typingHeld(id) || d.holdingMessages(id) {
		t.Fatal("still held after the dismissal")
	}
	until(t, "the queued text", func() bool { return strings.Contains(f.written(), "FIRSTQUEUED") })
}

// Several queued messages go in the order they were queued, after the wake prompt.
func TestQueuedTextGoesInOrderAfterTheWake(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	sayViaMessage(t, d, "", id, "QUEUEDONE")
	time.Sleep(5 * time.Millisecond)
	sayViaMessage(t, d, "", id, "QUEUEDTWO")
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["queued"] != 2 {
		t.Fatalf("two queued, the card says %v", d.newContextFor(id))
	}
	finishCycle(t, d, id, dir, f)
	d.wake.sawSession(id, time.Now())
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
