package daemon

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card that never leaves running is asked to end its turn, while the capture
// itself still waits for the gap. See newContextStop.

func ncBusy(d *Daemon, id string) {
	_ = d.st.SetStatus(id, store.StatusRunning)
	d.act.set(id, ActivityTool, "Bash")
}

func TestNewContextAsksARunningCardToEndItsTurn(t *testing.T) {
	fastNewContext(t)
	ncTiming.nudgeAfter = 100 * time.Millisecond
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	ncBusy(d, id)

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the stop request", func() bool { return strings.Contains(f.written(), newContextStop) })
	if strings.Contains(f.written(), "HANDOFF.") {
		t.Fatalf("the capture was typed into a running turn: %q", f.written())
	}
	// Typed by the cycle while every other delivery path is held.
	if !d.holdingMessages(id) {
		t.Fatal("the card is not held, so the stop request proves nothing about the hold")
	}
	until(t, "the chip to say why it waits", func() bool {
		v, _ := d.newContextFor(id).(map[string]any)
		label, _ := v["label"].(string)
		return strings.Contains(label, "waiting for the turn to end, asked the card to stop at")
	})

	ncTurnEnds(d, id)
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	got := f.written()
	if strings.Index(got, newContextStop) > strings.Index(got, "HANDOFF.") {
		t.Fatalf("the stop request came after the capture: %q", got)
	}
	// The capture is typed, so the chip no longer says it is waiting on a turn.
	until(t, "the chip to stop saying it waits on a turn", func() bool {
		v, _ := d.newContextFor(id).(map[string]any)
		label, _ := v["label"].(string)
		return !strings.Contains(label, "asked the card")
	})
}

// Twice, then the step fails at its limit naming both.
func TestNewContextAsksTwiceThenFails(t *testing.T) {
	fastNewContext(t)
	ncTiming.nudgeAfter = 100 * time.Millisecond
	ncTiming.captureEnd = 600 * time.Millisecond
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	id := task.ID
	ncBusy(d, id)

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
	if n := strings.Count(f.written(), newContextStop); n != 2 {
		t.Fatalf("typed the stop request %d times, want 2: %q", n, f.written())
	}
	r := failedWith(d, id)
	if !regexp.MustCompile(`was asked to end its turn at \d\d:\d\d and \d\d:\d\d and did not`).MatchString(r) {
		t.Fatalf("the reason does not name both stop requests: %q", r)
	}
	if strings.Contains(f.written(), "HANDOFF.") {
		t.Fatalf("the capture was typed into a turn that never ended: %q", f.written())
	}
}

// A card between turns is never asked.
func TestNewContextDoesNotAskAnIdleCard(t *testing.T) {
	fastNewContext(t)
	ncTiming.nudgeAfter = 0
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	if strings.Contains(f.written(), newContextStop) {
		t.Fatalf("asked a card between turns to stop: %q", f.written())
	}
}
