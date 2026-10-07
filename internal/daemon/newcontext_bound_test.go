package daemon

import (
	"strings"
	"testing"
	"time"
)

// The agent never runs `atrium ready`: the hold lifts by itself, says why on the
// card, and nothing is typed past the limit prompt.
func TestNewContextAckNeverComesReleasesTheHold(t *testing.T) {
	fastNewContext(t)
	ncTiming.ackWait = 300 * time.Millisecond
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	if !d.holdingMessages(task.ID) {
		t.Fatal("the cycle did not hold the card")
	}
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, "held for") || !strings.Contains(r, "released") {
		t.Fatalf("the card does not say why it was released: %q", r)
	}
	if d.holdingMessages(task.ID) {
		t.Fatal("the hold is still on after the bound")
	}
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("typed /clear without an ack: %q", f.written())
	}
}

// The process dies while the cycle waits: the hold goes with it.
func TestNewContextProcessDiesReleasesTheHold(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	d.sup.remove(task.ID)
	until(t, "the hold to lift", func() bool { return !d.holdingMessages(task.ID) })
	if r := failedWith(d, task.ID); !strings.Contains(r, "terminal closed") {
		t.Fatalf("reason: %q", r)
	}
}

// The room restarts mid-cycle: the run comes back as a failed chip, which holds nothing.
func TestNewContextFailedChipHoldsNothing(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	d.nctx.claim(task.ID, &newContext{step: NewContextLimit})
	gen := d.nctx.get(task.ID).gen
	d.nctx.fail(task.ID, gen, "the room restarted")
	if d.holdingMessages(task.ID) {
		t.Fatal("a failed chip holds messages")
	}
}
