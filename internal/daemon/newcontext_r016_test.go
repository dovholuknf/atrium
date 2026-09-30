package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-016: a new-context cycle lets nothing land between its steps, and its failed
// chip clears only on proof. See newcontext.go.

// failedCycle runs a cycle on a card that started in conversation "conv-A" and
// waits for it to fail (the capture prompt starts no turn).
func failedCycle(t *testing.T, d *Daemon, id string) {
	t.Helper()
	d.wakeSawSession(id, "conv-A")
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
}

func TestFailedChipClearsOnASessionStartWithANewConversation(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	id := task.ID
	failedCycle(t, d, id)

	// The same conversation starting again is not proof.
	d.wakeSawSession(id, "conv-A")
	if failedWith(d, id) == "" {
		t.Fatal("the chip cleared on a SessionStart in the same conversation")
	}
	d.wakeSawSession(id, "")
	if failedWith(d, id) == "" {
		t.Fatal("the chip cleared on a SessionStart with no conversation id")
	}
	// A new conversation is.
	d.wakeSawSession(id, "conv-B")
	if d.newContextFor(id) != nil {
		t.Fatalf("the chip stayed after a new conversation began: %v", d.newContextFor(id))
	}

	// The reason stays in the card's history.
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range evs {
		if e.Kind == store.EventNotified && strings.Contains(string(e.Payload), newContextBy) &&
			strings.Contains(string(e.Payload), "failed") {
			found = true
		}
	}
	if !found {
		t.Fatal("the failure reason is not in the card's history")
	}
}

func TestFailedChipIsClearedByRerunAndByDismissal(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	failedCycle(t, d, id)

	before := strings.Count(f.written(), "HANDOFF.")
	if err := d.StartNewContext(id); err != nil {
		t.Fatalf("could not rerun over a failed chip: %v", err)
	}
	if failedWith(d, id) != "" {
		t.Fatal("the rerun did not replace the failed chip")
	}
	until(t, "the second capture prompt", func() bool { return strings.Count(f.written(), "HANDOFF.") > before })
	until(t, "the second failure", func() bool { return failedWith(d, id) != "" })

	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.handleNewContext(rec, req)
	if rec.Code != http.StatusOK || d.newContextFor(id) != nil {
		t.Fatalf("dismiss left the chip: %d %s", rec.Code, rec.Body)
	}
}

// The @ui case: a peer message starts a turn, and that proves nothing.
func TestFailedChipSurvivesATurnStart(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	id := task.ID
	failedCycle(t, d, id)

	d.act.set(id, ActivityThinking, "")
	d.act.promptSeen(id)
	d.turnResumed(id)
	ncTurnEnds(d, id)
	time.Sleep(30 * time.Millisecond)
	if failedWith(d, id) == "" {
		t.Fatal("a turn start cleared the failed chip")
	}
}

// Reaching the wake with a turn already running (a peer message started one after
// the clear) waits it out rather than failing at typeWait.
func TestWakeWaitsOutATurnInProgress(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	finishCycle(t, d, id, dir, f)
	d.act.set(id, ActivityThinking, "")
	d.wakeSawSession(id, "conv-B")

	// Longer than typeWait, which is what used to give up.
	time.Sleep(ncTiming.typeWait + 200*time.Millisecond)
	if failedWith(d, id) != "" {
		t.Fatalf("the cycle failed while a turn ran: %s", failedWith(d, id))
	}
	if strings.Contains(f.written(), ncWakeMark) {
		t.Fatalf("the wake was typed into a turn: %q", f.written())
	}
	ncTurnEnds(d, id)
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
}

func TestWakeFailsWhenNoGapOpensInCaptureEnd(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	finishCycle(t, d, id, dir, f)
	ncTiming.captureEnd = 500 * time.Millisecond
	d.act.set(id, ActivityThinking, "")
	d.wakeSawSession(id, "conv-B")

	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
	if r := failedWith(d, id); !strings.Contains(r, "wake") {
		t.Fatalf("the reason does not name the wake: %q", r)
	}
	if strings.Contains(f.written(), ncWakeMark) {
		t.Fatalf("the wake was typed anyway: %q", f.written())
	}
}

// os_WriteHandoff writes the card's own handoff file, as the capture turn does.
func os_WriteHandoff(d *Daemon, id, dir string) error {
	task, err := d.st.Get(id)
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, HandoffName(task)), handoffBody, 0o644)
}

// A prompt event a moment ago means the runner is taking input: wait it out.
func TestNcTypeWaitsTurnSettleAfterAPromptEvent(t *testing.T) {
	fastNewContext(t)
	ncTiming.turnSettle = 300 * time.Millisecond
	ncTiming.typeWait = 3 * time.Second
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	gen, ok := d.nctx.begin(id, HandoffName(task), "")
	if !ok {
		t.Fatal("could not begin")
	}

	d.act.promptSeen(id)
	time.Sleep(100 * time.Millisecond)
	start := time.Now()
	if err := d.ncType(id, gen, "", "/clear", ncTiming.typeWait); err != nil {
		t.Fatal(err)
	}
	if got := time.Since(start); got < 150*time.Millisecond {
		t.Fatalf("typed after %s, before the settle gap was over", got)
	}
	if !strings.Contains(f.written(), "/clear") {
		t.Fatalf("nothing typed: %q", f.written())
	}
}

// The injector firing inside turnSettle: /clear first, the message after the wake.
func TestInjectorDuringTurnSettleTypesClearFirst(t *testing.T) {
	fastNewContext(t)
	ncTiming.turnSettle = 300 * time.Millisecond
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(id, ActivityThinking, "")
	d.act.promptSeen(id)
	if err := os_WriteHandoff(d, id, dir); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	ncTurnEnds(d, id)

	// Inside the settle gap the injector is kicked repeatedly.
	sayViaMessage(t, d, "alice", id, heldSay)
	for i := 0; i < 10 && !strings.Contains(f.written(), "/clear"); i++ {
		d.pending.reset(id)
		time.Sleep(20 * time.Millisecond)
	}
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("the message was typed ahead of /clear or with it: %q", f.written())
	}

	d.wakeSawSession(id, "conv-B")
	until(t, "the held say", func() bool { return strings.Contains(f.written(), heldSay) })
	got := f.written()
	if strings.Index(got, "/clear") > strings.Index(got, ncWakeMark) || strings.Index(got, ncWakeMark) > strings.Index(got, heldSay) {
		t.Fatalf("out of order: %q", got)
	}
}

// The fabric sequence: a queued peer message and the capture turn's end together.
func TestQueuedMessageAndCaptureEndTogetherTypeClearAlone(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")
	d.wakeSawSession(id, "conv-A")

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	d.act.set(id, ActivityThinking, "")
	if err := os_WriteHandoff(d, id, dir); err != nil {
		t.Fatal(err)
	}
	time.Sleep(60 * time.Millisecond)
	// At the same moment.
	sayViaMessage(t, d, "alice", id, heldSay)
	ncTurnEnds(d, id)
	d.pending.reset(id)

	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("the message went in with /clear: %q", f.written())
	}
	time.Sleep(60 * time.Millisecond)
	if strings.Contains(f.written(), heldSay) {
		t.Fatalf("the message was typed before the new session: %q", f.written())
	}
	// A new session starts.
	d.wakeSawSession(id, "conv-B")
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
}
