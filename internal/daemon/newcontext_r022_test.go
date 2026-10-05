package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-022: a runner that loses a line typed mid-turn must not get the limit prompt
// until the card is between turns. The card here reads idle in its activity (a turn
// that ended on background work) while its status still says running, which is the
// gap @ui's cycles fell into.
func TestACycleOnARunnerWithoutMidTurnInputWaitsForTheTurnToEnd(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	h, err := d.st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.MidTurnInput = false
	if _, err := d.st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.act.set(task.ID, ActivityIdle, "")

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(f.written(), limitPrompt) {
		t.Fatalf("the limit prompt was typed while the card was running: %q", f.written())
	}

	// The running turn ends. Only now is it typed.
	ncTurnEnds(d, task.ID)
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
}
