package store

import (
	"path/filepath"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "atrium.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRegisterIsIdempotentByWireName(t *testing.T) {
	s := open(t)
	obs := Observed{WireName: "atrium-101", Worktree: "d:/git/atrium", Runner: "claude", PID: 101}

	first, created, err := s.Register(obs)
	if err != nil || !created {
		t.Fatalf("first register: created=%v err=%v", created, err)
	}
	second, created, err := s.Register(obs)
	if err != nil {
		t.Fatalf("second register: %v", err)
	}
	if created {
		t.Fatal("second register created a new task")
	}
	if first.ID != second.ID {
		t.Fatalf("task id changed across reconnect: %s then %s", first.ID, second.ID)
	}
}

// A new pid for the same wire name must land on the same card. This is the
// whole reason pid is not the identity.
func TestRegisterSurvivesPIDChange(t *testing.T) {
	s := open(t)
	first, _, err := s.Register(Observed{WireName: "agent-a", Worktree: "d:/w", Runner: "claude", PID: 1})
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := s.Register(Observed{WireName: "agent-a", Worktree: "d:/w", Runner: "claude", PID: 2})
	if err != nil {
		t.Fatal(err)
	}
	if created || first.ID != second.ID {
		t.Fatalf("restart split the card: created=%v %s vs %s", created, first.ID, second.ID)
	}
	if second.PID != 2 {
		t.Fatalf("observed pid not refreshed: %d", second.PID)
	}
}

func TestOverrideSurvivesReconnect(t *testing.T) {
	s := open(t)
	obs := Observed{WireName: "agent-b", Worktree: "d:/w", Runner: "claude", PID: 7}
	task, _, err := s.Register(obs)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOverrides(task.ID, map[string]string{"title": "fix the auth bug"}); err != nil {
		t.Fatal(err)
	}
	// Runner reconnects and reports its observed title again.
	again, _, err := s.Register(obs)
	if err != nil {
		t.Fatal(err)
	}
	if got := again.DisplayTitle(); got != "fix the auth bug" {
		t.Fatalf("observed data clobbered the override: %q", got)
	}
	// Clearing the override falls back to observed.
	if err := s.SetOverrides(task.ID, map[string]string{"title": ""}); err != nil {
		t.Fatal(err)
	}
	back, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := back.DisplayTitle(); got != "agent-b" {
		t.Fatalf("clearing override did not fall back: %q", got)
	}
}

func TestWaitingIsOldestFirst(t *testing.T) {
	s := open(t)
	var ids []string
	for _, name := range []string{"one", "two", "three"} {
		task, _, err := s.Register(Observed{WireName: name, Worktree: "d:/" + name, Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
		if err := s.SetStatus(task.ID, StatusNeedsInput); err != nil {
			t.Fatal(err)
		}
	}
	waiting, err := s.Waiting()
	if err != nil {
		t.Fatal(err)
	}
	if len(waiting) != 3 {
		t.Fatalf("want 3 waiting, got %d", len(waiting))
	}
	// Registration order is wait order, since each entered needs-input in turn.
	for i, want := range ids {
		if waiting[i].ID != want {
			t.Fatalf("position %d: want %s, got %s", i, want, waiting[i].ID)
		}
	}
	for _, w := range waiting {
		if w.WaitingSince == nil {
			t.Fatalf("task %s in needs-input with no waiting_since", w.ID)
		}
	}
}

// Re-entering a waiting state must not reset the clock, or a permission round
// trip would push a long-waiting card to the back of the stack.
func TestWaitingSinceSurvivesPermissionRoundTrip(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "agent-c", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	before, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}
	after, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.WaitingSince.Equal(*before.WaitingSince) {
		t.Fatalf("waiting clock reset: %v then %v", before.WaitingSince, after.WaitingSince)
	}
	if err := s.SetStatus(task.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	running, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if running.WaitingSince != nil {
		t.Fatal("waiting_since not cleared on return to running")
	}
}

func TestPermissionDedup(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "agent-d", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	first, decided, err := s.RecordPermission(task.ID, "Bash", "rm -rf /", "key-1", "")
	if err != nil || decided {
		t.Fatalf("first record: decided=%v err=%v", decided, err)
	}
	if _, err := s.DecidePermission(first.ID, "block", "no"); err != nil {
		t.Fatal(err)
	}
	// Agent replays the same request after a restart.
	again, decided, err := s.RecordPermission(task.ID, "Bash", "rm -rf /", "key-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if !decided {
		t.Fatal("replayed request came back undecided, so the operator would be asked twice")
	}
	if again.ID != first.ID || again.Decision != "block" {
		t.Fatalf("replay lost the decision: %+v", again)
	}
	pending, err := s.PendingPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("want no pending, got %d", len(pending))
	}
}

func TestRankPlacesNewCardsOnTop(t *testing.T) {
	s := open(t)
	var ranks []float64
	for _, name := range []string{"first", "second", "third"} {
		task, _, err := s.Register(Observed{WireName: name, Worktree: "d:/" + name, Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		ranks = append(ranks, task.Rank)
	}
	for i := 1; i < len(ranks); i++ {
		if ranks[i] >= ranks[i-1] {
			t.Fatalf("new card did not sort above the previous: %v", ranks)
		}
	}
	listed, err := s.List(StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	if len(listed) != 3 || listed[0].WireName != "third" {
		t.Fatalf("board order wrong: %+v", listed)
	}
	// Midpoint insertion moves a card without renumbering its neighbours.
	mid := (listed[0].Rank + listed[1].Rank) / 2
	if err := s.SetRank(listed[2].ID, mid); err != nil {
		t.Fatal(err)
	}
	listed, err = s.List(StatusRunning)
	if err != nil {
		t.Fatal(err)
	}
	if listed[1].WireName != "first" {
		t.Fatalf("midpoint insert landed wrong: %+v", listed)
	}
}

func TestEventsRecordHistory(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "agent-e", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	events, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 {
		t.Fatalf("want created plus status-changed, got %d", len(events))
	}
	if events[0].Kind != EventCreated {
		t.Fatalf("first event should be created, got %s", events[0].Kind)
	}
}

func TestWedgeRefusesFurtherWork(t *testing.T) {
	s := open(t)
	var gotCause error
	s.OnWedge = func(cause error) { gotCause = cause }

	// Close the underlying database to force a non-contention failure.
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(Observed{WireName: "x", Worktree: "d:/x", Runner: "claude"}); err == nil {
		t.Fatal("expected an error once the database is unusable")
	}
	wedged, cause := s.Wedged()
	if !wedged {
		t.Fatal("store did not wedge on a hard failure")
	}
	if cause == nil || gotCause == nil {
		t.Fatalf("wedge cause not reported: cause=%v callback=%v", cause, gotCause)
	}
	// Every later call must refuse rather than retry.
	if _, err := s.List(); err == nil {
		t.Fatal("wedged store still served a read")
	}
}
