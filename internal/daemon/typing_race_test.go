package daemon

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

// A peer message that arrives mid-turn is not typed, is not carried by the hooks,
// and is typed and sent once the turn ends. Typed mid-turn, Claude Code holds it
// and sends it with whatever the operator types before the turn ends, as one
// prompt. See docs/typing-race.md.
func TestAPeerMessageMidTurnWaitsForTheTurnToEnd(t *testing.T) {
	d := testDaemon(t)
	target, _, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	// The line is empty and nobody has typed, so only the turn holds it.
	d.act.set(target.ID, ActivityTool, "Bash")

	typed, err := d.deliverPeer(target, "sg4/doer", "neither matrix exists")
	if err != nil {
		t.Fatal(err)
	}
	if typed || f.written() != "" {
		t.Fatalf("typed into a runner mid-turn: %q", f.written())
	}
	if heldCount(d.pending, target.ID) != 1 {
		t.Fatal("the message was not held for the terminal")
	}
	// The next tool call's hook leaves it alone.
	msgs, err := d.takeMessages(target.ID, "permission")
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 0 {
		t.Fatalf("the permission hook carried a message held for the terminal: %d", len(msgs))
	}

	// Still mid-turn: a retry types nothing and does not widen the backoff.
	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("a retry typed into a runner mid-turn: %q", f.written())
	}
	if got := heldStep(d.pending, target.ID); got != 0 {
		t.Fatalf("waiting out a turn widened the backoff: step %d", got)
	}

	// The turn ends. The Stop hook also leaves it for the typist.
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

// A peer message that arrives while the operator's line has text waits, and
// goes in once the operator clears the line and stops typing.
func TestAPeerMessageWaitsForTheLineToEmpty(t *testing.T) {
	d := testDaemon(t)
	target, r, f := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	partial := "it could be that was th"
	r.noteOperatorTyped([]byte(partial))

	if typed, _ := d.deliverPeer(target, "sg4/doer", "the build is green"); typed {
		t.Fatal("typed into a part written line")
	}
	d.pending.attempt(target.ID)
	if f.written() != "" {
		t.Fatalf("a retry typed into a part written line: %q", f.written())
	}
	if msgs, _ := d.takeMessages(target.ID, "permission"); len(msgs) != 0 {
		t.Fatalf("a hook carried a message held for the terminal: %d", len(msgs))
	}

	// The operator backspaces the line to nothing and walks away.
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

// The operator's own channel keeps its rule: mid-turn is not a reason to hold
// it. Only the line gate applies.
func TestTheOperatorsTextIsNotHeldForTheTurn(t *testing.T) {
	d := testDaemon(t)
	target, _, _ := peerPair(t, d)
	d.act.set(target.ID, ActivityThinking, "")
	if d.peerMustWait(target.ID, "") {
		t.Fatal("the operator's own text was held for the turn")
	}
	if !d.peerMustWait(target.ID, "sg4/doer") {
		t.Fatal("a peer's text was not held for the turn")
	}
}
