//go:build integration

package daemon

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func staleRunningCard(t *testing.T, d *Daemon, frame string) (*store.Task, *fakePTY) {
	t.Helper()
	task, f, _ := ncCard(t, d)
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	d.act.set(task.ID, ActivityThinking, "")
	run := d.sup.get(task.ID)
	_, _ = run.buf.Write([]byte(frame))
	run.lastOut.Store(time.Now().Add(-time.Minute).UnixNano())
	return task, f
}

func TestNewContextClearsAtOnceOnAnIdleCardWithALostStop(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f := staleRunningCard(t, d, idleFrame)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return strings.Contains(f.written(), limitPrompt) })
	ncAck(t, d, task)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
}

// Plan 5: the limit prompt goes mid-turn, the way an immediate say does. /clear does not.
func TestNewContextAsksMidTurnButClearsOnlyBetweenTurns(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f := staleRunningCard(t, d, workingFrame)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt mid-turn", func() bool { return strings.Contains(f.written(), limitPrompt) })
	ncAck(t, d, task)
	time.Sleep(150 * time.Millisecond)
	if got := f.written(); strings.Contains(got, "/clear") {
		t.Fatalf("typed /clear into a card whose screen shows a turn: %q", got)
	}
}

func TestNewContextBusyNamesTheStepAndHowLong(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, _ := staleRunningCard(t, d, workingFrame)

	if err := d.StartNewContext(task.ID); err != nil {
		t.Fatal(err)
	}
	err := d.StartNewContext(task.ID)
	if !errors.Is(err, errNewContextBusy) {
		t.Fatalf("wanted busy, got %v", err)
	}
	if msg := err.Error(); !strings.Contains(msg, "step 1 of 3 (limit) for ") {
		t.Fatalf("the refusal does not name the step and how long: %q", msg)
	}
}
