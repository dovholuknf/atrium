package daemon

import (
	"strings"
	"testing"
)

// r-025: a message held by a new-context cycle says so, and is not blamed on an
// empty line.
func TestHeldDuringCaptureReportsNewContext(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	peerCard(t, d, "alice")
	t.Cleanup(func() { d.pending.stopAll() })

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	sayViaMessage(t, d, "alice", id, heldSay)
	sayViaMessage(t, d, "alice", id, heldSay)
	a := d.act.get(id)
	if a == nil || a.HeldFor != HeldForNewContext || a.HeldCount == 0 {
		t.Fatalf("a message held by the cycle is not reported as new-context: %+v", a)
	}
	// A retry inside the cycle keeps saying so.
	d.pending.attempt(id)
	if a := d.act.get(id); a == nil || a.HeldFor != HeldForNewContext {
		t.Fatalf("a retry inside the cycle changed the reason: %+v", a)
	}
}

// Held behind a shut line still reports the line, and an open gate with nothing
// else holding never does.
func TestHeldForLineOnlyWhenTheGateIsShut(t *testing.T) {
	d := testDaemon(t)
	target, r, _ := peerPair(t, d)
	t.Cleanup(func() { d.pending.stopAll() })
	r.noteOperatorTyped([]byte("half a thought"))
	if typed, _ := d.deliverPeer(target, "sg4/doer", "hello"); typed {
		t.Fatal("typed into a part written line")
	}
	if a := d.act.get(target.ID); a == nil || a.HeldFor != HeldForLine {
		t.Fatalf("a shut line is not reported as the line: %+v", a)
	}
	m := pendingMsg{}
	if got := d.pending.heldFor(target.ID, m); got != HeldForLine {
		t.Fatalf("heldFor = %q behind a shut line", got)
	}
	r.noteOperatorTyped([]byte{0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f, 0x7f})
	r.typeMu.Lock()
	r.lastTyped = r.lastTyped.Add(-peerGateIdle * 2)
	r.typeMu.Unlock()
	if got := d.pending.heldFor(target.ID, m); got != "" {
		t.Fatalf("heldFor = %q with the gate open and nothing else holding", got)
	}
}
