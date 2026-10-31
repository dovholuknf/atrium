//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"strings"
	"testing"
	"time"
)

// The agent never runs `atrium ready`: the hold lifts by itself, says why on the
// card, and nothing is typed past the limit prompt.
func TestNewContextAckNeverComesReleasesTheHold(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	d.hk.NcTiming.AckWait = 300 * time.Millisecond
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	if !d.HoldingMessages(task.ID) {
		t.Fatal("the cycle did not hold the card")
	}
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	if r := failedWith(d, task.ID); !strings.Contains(r, "held for") || !strings.Contains(r, "released") {
		t.Fatalf("the card does not say why it was released: %q", r)
	}
	if d.HoldingMessages(task.ID) {
		t.Fatal("the hold is still on after the bound")
	}
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("typed /clear without an ack: %q", f.written())
	}
}

// The process dies while the cycle waits: the hold goes with it.
func TestNewContextProcessDiesReleasesTheHold(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, _, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	d.sup.remove(task.ID)
	until(t, "the chip to fail", func() bool { return failedWith(d, task.ID) != "" })
	until(t, "the hold to lift", func() bool { return !d.HoldingMessages(task.ID) })
	if r := failedWith(d, task.ID); !strings.Contains(r, "terminal closed") {
		t.Fatalf("reason: %q", r)
	}
}

// The room restarts mid-cycle: the run comes back as a failed chip, which holds nothing.
func TestNewContextFailedChipHoldsNothing(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, _, _ := ncCard(t, d)
	d.Nctx.Claim(task.ID, &cycle.NewContext{Step: cycle.NewContextLimit})
	gen := d.Nctx.Get(task.ID).Gen
	d.Nctx.Fail(task.ID, gen, "the room restarted")
	if d.HoldingMessages(task.ID) {
		t.Fatal("a failed chip holds messages")
	}
}
