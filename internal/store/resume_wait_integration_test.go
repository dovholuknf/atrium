//go:build integration

package store

import (
	"testing"
	"time"
)

// A card a deploy took down while it waited keeps its wait when it comes back and its resume turn ends. Otherwise the
// phone and the board ring "waiting for you" for a card that was already waiting before the deploy.
func TestResumedCardKeepsItsEarlierWaitingSince(t *testing.T) {
	t.Parallel()
	s := open(t)
	base := time.Date(2026, 1, 1, 9, 0, 0, 0, time.UTC)
	at := base
	s.clock = func() time.Time { return at }

	task, _, err := s.Register(Observed{WireName: "agent-r", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	set := func(status, reason string) {
		t.Helper()
		if err := s.SetStatusBecause(task.ID, status, reason); err != nil {
			t.Fatal(err)
		}
	}
	since := func() time.Time {
		t.Helper()
		g, err := s.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if g.WaitingSince == nil {
			t.Fatalf("no waiting_since in %s", g.Status)
		}
		return *g.WaitingSince
	}

	set(StatusNeedsInput, "")
	first := since()

	// The deploy: the runner goes down, then comes back and runs one turn.
	at = base.Add(time.Hour)
	set(StatusDead, "")
	at = base.Add(2 * time.Hour)
	set(StatusNeedsInput, WaitingStarted)
	if got := since(); !got.Equal(first) {
		t.Fatalf("resume reset the wait on coming up: %v, want %v", got, first)
	}
	at = base.Add(3 * time.Hour)
	set(StatusRunning, "")
	at = base.Add(4 * time.Hour)
	set(StatusNeedsInput, "")
	if got := since(); !got.Equal(first) {
		t.Fatalf("resume turn end reset the wait: %v, want %v", got, first)
	}

	// A person answers: the next wait is a new one.
	if err := s.ClearCarriedWait(task.ID); err != nil {
		t.Fatal(err)
	}
	set(StatusRunning, "")
	at = base.Add(5 * time.Hour)
	set(StatusNeedsInput, "")
	if got := since(); !got.Equal(at) {
		t.Fatalf("a new wait after input kept the old clock: %v, want %v", got, at)
	}
}
