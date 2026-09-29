package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-022: a cycle armed while the card is running must not type its capture until
// the card is between turns. The card here reads idle in its activity (a turn
// that ended on background work) while its status still says running, which is
// the gap @ui's cycles fell into: the capture was typed into it, and the running
// turn's Stop was taken as the capture's.
func TestACycleArmedWhileRunningWaitsForTheTurnToEnd(t *testing.T) {
	fastNewContext(t)
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task, f, _ := ncCard(t, d)
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.act.set(task.ID, ActivityIdle, "")

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	time.Sleep(300 * time.Millisecond)
	if strings.Contains(f.written(), "HANDOFF.") {
		t.Fatalf("the capture was typed while the card was running: %q", f.written())
	}

	// The running turn ends. Only now is the capture typed.
	ncTurnEnds(d, task.ID)
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
}
