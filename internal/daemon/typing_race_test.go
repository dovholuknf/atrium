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

// IMMEDIATE IS THE DEFAULT. A peer message that arrives mid-turn is typed and
// sent as soon as the line is empty, because Claude Code queues a line typed
// mid-turn and reads it at its next step. See saywhen.go.
func TestASayMidTurnIsTypedImmediatelyByDefault(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	typed, err := d.deliverPeer(target, "sg4/doer", "stop now")
	if err != nil {
		t.Fatal(err)
	}
	got := f.written()
	if !typed || !strings.Contains(got, "stop now") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("a say mid-turn was not typed and sent: typed %v, %q", typed, got)
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("an immediate say was held")
	}
}

// The same through the message endpoint `atrium_say` posts to, with no `when`.
func TestTheMessageEndpointTypesMidTurnWithNoWhen(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityThinking, "")

	if code := message(t, d, target.ID, map[string]string{"text": "stop now", "from": "main:atrium"}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	if !strings.Contains(f.written(), "stop now") {
		t.Fatalf("a say with no when was not typed mid-turn: %q", f.written())
	}
}

// `when: "done"` is the old rule for one message: not typed mid-turn, not
// carried by the hooks, and typed and sent once the turn ends.
func TestASayWhenDoneWaitsForTheTurnToEnd(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")

	if code := message(t, d, target.ID, map[string]string{
		"text": "neither matrix exists", "from": "sg4/doer", "when": "done",
	}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	if f.written() != "" {
		t.Fatalf("typed a done message mid-turn: %q", f.written())
	}
	if heldCount(d.pending, target.ID) != 1 {
		t.Fatal("the message was not held for the terminal")
	}
	if a := d.act.get(target.ID); a == nil || a.HeldFor != HeldForTurn {
		t.Fatalf("the held signal does not say the turn is holding it: %+v", a)
	}
	// The next tool call's hook leaves it alone.
	if msgs, _ := d.takeMessages(target.ID, "permission"); len(msgs) != 0 {
		t.Fatalf("the permission hook carried a done message: %d", len(msgs))
	}

	// Still mid-turn: a retry types nothing and does not widen the backoff.
	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("a retry typed into a runner mid-turn: %q", f.written())
	}
	if got := heldStep(d.pending, target.ID); got != 0 {
		t.Fatalf("waiting out a turn widened the backoff: step %d", got)
	}

	// The turn ends. The Stop hook leaves it for the typist.
	d.onActivity(ActivityEvent{TaskID: target.ID, Event: "idle"})
	if msgs, _ := d.takeMessages(target.ID, "stop"); len(msgs) != 0 {
		t.Fatalf("the Stop hook carried a message held for the terminal: %d", len(msgs))
	}
	d.pending.attempt(target.ID)

	got := f.written()
	if !strings.Contains(got, "neither matrix exists") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("the message was not typed and sent after the turn ended: %q", got)
	}
	if heldCount(d.pending, target.ID) != 0 {
		t.Fatal("the hold was not cleared after landing")
	}
	if pending, _ := d.st.PendingMessages(target.ID); len(pending) != 0 {
		t.Fatalf("the landed message was not marked delivered: %d pending", len(pending))
	}
}

// With no terminal to type into, a done message is the Stop hook's alone. The
// permission hook fires mid-turn, and carrying it there is the interruption it
// asked not to have.
func TestADoneMessageRidesOnlyTheStopHook(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")

	if code := message(t, d, bob.ID, map[string]string{
		"text": "when you are done, rebase", "from": "alice", "when": "done",
	}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 0 {
		t.Fatalf("the permission hook carried a done message: %d", len(msgs))
	}
	msgs, err := d.takeMessages(bob.ID, "stop")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 1 || !msgs[0].WaitTurn {
		t.Fatalf("the Stop hook did not carry the done message: %+v", msgs)
	}
}

// And an immediate one rides the next tool call, the way it always has.
func TestAnImmediateMessageRidesThePermissionHook(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	if code := message(t, d, bob.ID, map[string]string{"text": "stop now", "from": "alice"}); code != http.StatusOK {
		t.Fatalf("message answered %d", code)
	}
	if msgs, _ := d.takeMessages(bob.ID, "permission"); len(msgs) != 1 {
		t.Fatalf("the permission hook did not carry an immediate message: %d", len(msgs))
	}
}

// A runner set not to take input mid-turn falls back to done for every
// message, the default included.
func TestARunnerThatTakesNoMidTurnInputFallsBackToDone(t *testing.T) {
	d := testDaemon(t)
	h, err := d.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.MidTurnInput = false
	if _, err := d.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityThinking, "")

	if typed, _ := d.deliverPeer(target, "sg4/doer", "stop now"); typed || f.written() != "" {
		t.Fatalf("typed mid-turn into a runner that does not take it: %q", f.written())
	}
	if a := d.act.get(target.ID); a == nil || a.HeldFor != HeldForTurn {
		t.Fatalf("the held signal does not say the turn is holding it: %+v", a)
	}
	d.onActivity(ActivityEvent{TaskID: target.ID, Event: "idle"})
	d.pending.attempt(target.ID)
	if !strings.Contains(f.written(), "stop now") {
		t.Fatalf("the message was not typed once the turn ended: %q", f.written())
	}
}

// Immediate still goes through the gate: a line with text holds it, and a
// cleared line lets it through.
func TestAnImmediateSayWaitsForTheLineToEmpty(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.set(target.ID, ActivityTool, "Bash")
	partial := "it could be that was th"
	r.noteOperatorTyped([]byte(partial))

	if typed, _ := d.deliverPeer(target, "sg4/doer", "the build is green"); typed {
		t.Fatal("typed into a part written line")
	}
	if a := d.act.get(target.ID); a == nil || a.HeldFor != HeldForLine || a.HeldCount != 1 {
		t.Fatalf("the held signal does not say the line is holding it: %+v", a)
	}
	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("a retry typed into a part written line: %q", f.written())
	}
	if msgs, _ := d.takeMessages(target.ID, "permission"); len(msgs) != 0 {
		t.Fatalf("a hook carried a message held for the terminal: %d", len(msgs))
	}

	// The operator backspaces the line to nothing and walks away. Still
	// mid-turn, and an immediate message does not care.
	r.noteOperatorTyped(bytes.Repeat([]byte{0x7f}, len(partial)))
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()
	d.pending.attempt(target.ID)

	got := f.written()
	if !strings.Contains(got, "the build is green") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("a cleared line did not let the message through: %q", got)
	}
}

// Immediate still goes through the gate: an open dialog holds it, and the
// held signal says so rather than blaming the line.
func TestAnImmediateSayWaitsForAnOpenDialog(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	d.act.dialogRaised(target.ID)

	if typed, _ := d.deliverPeer(target, "sg4/doer", "stop now"); typed || f.written() != "" {
		t.Fatalf("typed into a terminal with a dialog open: %q", f.written())
	}
	if a := d.act.get(target.ID); a == nil || a.HeldFor != HeldForDialog {
		t.Fatalf("the held signal does not say a dialog is holding it: %+v", a)
	}
}

// Two held messages are counted, which the chip shows as `! 2`.
func TestTheHeldSignalCountsTheMessages(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a thought"))
	for _, text := range []string{"first", "second"} {
		if typed, _ := d.deliverPeer(target, "sg4/doer", text); typed {
			t.Fatal("typed into a part written line")
		}
	}
	if a := d.act.get(target.ID); a == nil || a.HeldCount != 2 || a.HeldPeer != "sg4/doer" {
		t.Fatalf("the held signal does not count two messages: %+v", a)
	}
}

// A done message to a card that cannot be typed into and has never shown a
// Stop hook would wait forever, so the sender is told.
func TestADoneMessageNothingWillCarrySaysSo(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	if err := d.st.SawHook(bob.ID, store.HookTool); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]string{"text": "after", "from": "alice", "when": "done"})
	req := httptest.NewRequest("POST", "/v1/tasks/"+bob.ID+"/message", bytes.NewReader(raw))
	req.SetPathValue("id", bob.ID)
	rec := httptest.NewRecorder()
	d.handleMessage(rec, req)
	var out struct {
		When    string `json:"when"`
		Warning string `json:"warning"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	if out.When != WhenDone || !strings.Contains(out.Warning, "Stop hook") {
		t.Fatalf("a done message nothing will carry did not say so: %s", rec.Body)
	}
}

// A `when` that is neither word is refused, rather than read as the default.
func TestAnUnknownWhenIsRefused(t *testing.T) {
	d := testDaemon(t)
	bob := peerCard(t, d, "bob")
	if code := message(t, d, bob.ID, map[string]string{"text": "x", "from": "alice", "when": "later"}); code != http.StatusBadRequest {
		t.Fatalf("an unknown when answered %d", code)
	}
}

// The operator's text follows the same rule: immediate unless it asks to wait.
func TestTheOperatorsTextFollowsTheSameRule(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	d.act.set(target.ID, ActivityThinking, "")
	if d.turnHolds(target.ID, d.waitsForTurn(target.ID, WhenImmediate)) {
		t.Fatal("an immediate message was held for the turn")
	}
	if !d.turnHolds(target.ID, d.waitsForTurn(target.ID, WhenDone)) {
		t.Fatal("a done message was not held for the turn")
	}
}
