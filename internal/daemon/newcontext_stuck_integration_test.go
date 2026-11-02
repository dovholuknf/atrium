//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-new-context-stuck-at-clearing: the agent ran `atrium ready` and its turn ended
// (the Stop hook landed, the activity reads idle), but it had subagents out, so the
// card's status stayed running and the screen never read settled. /clear was held for
// the whole of captureEnd with nothing logged.
func TestClearIsTypedWhenTheTurnEndedWithSubagentsStillOut(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })

	d.act.set(id, ActivityTool, "Bash")
	_ = d.st.SetStatus(id, store.StatusRunning)
	ncAck(t, d, task)
	// The turn ends with a subagent out: activity idle, status still running.
	d.act.subagentStarted(id, "sub-1", "general")
	d.act.set(id, ActivityIdle, "")

	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

// A /clear that can never be typed fails the cycle with the reason, instead of sitting
// on step 2.
func TestClearThatCannotBeTypedFailsWithWhatBlockedIt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	d.hk.NcTiming.ClearTypeWait = 300 * time.Millisecond
	task, f, _ := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.act.set(id, ActivityTool, "Bash")
	ncAck(t, d, task)

	until(t, "the chip to fail", func() bool { return failedWith(d, id) != "" })
	r := failedWith(d, id)
	if !strings.Contains(r, "/clear") || !strings.Contains(r, "a turn is in progress") {
		t.Fatalf("the reason does not say what blocked /clear: %q", r)
	}
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("typed /clear into a turn: %q", f.written())
	}
}

// r-new-context-never-clears: the Stop hook never landed (lost, or not installed) and a background shell of the session
// keeps the screen redrawing, so the settled screen never reads idle. The card posts no activity after `atrium ready`,
// and /clear is typed once it has been quiet for ackQuiet, within seconds and not after clearTypeWait.
func TestClearIsTypedWhenTheStopWasLostAndTheCardWentQuiet(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	d.hk.NcTiming.AckQuiet = 300 * time.Millisecond
	d.hk.NcTiming.ClearTypeWait = time.Minute
	task, f, _ := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.act.set(id, ActivityTool, "Bash")
	ncAck(t, d, task)
	d.act.set(id, ActivityThinking, "")

	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

// While /clear waits, the card says what it waits on, rather than sitting on step 2 in silence.
func TestClearWaitSaysWhyOnTheCard(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	d.hk.NcTiming.ClearTypeWait = time.Minute
	task, f, _ := ncCard(t, d)
	id := task.ID

	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	d.act.set(id, ActivityTool, "Bash")
	ncAck(t, d, task)

	until(t, "the chip to say what it waits on", func() bool {
		c := d.Nctx.Get(id)
		return c != nil && c.Step == cycle.NewContextClear && strings.Contains(c.Reason, "a turn is in progress")
	})
}
