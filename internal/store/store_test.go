package store

import (
	"encoding/json"
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
	// Midpoint insertion moves a card without renumbering its neighbors.
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

// The limit takes the NEWEST events and hands them back oldest first.
//
// Both halves are asserted because they pull opposite ways and getting either
// one alone produces a plausible wrong answer: the oldest N in the right order,
// or the newest N upside down. The endpoint answers "what just happened", and
// for two years it answered with the day the card was created.
func TestEventsReturnsTheNewestWindowInTimeOrder(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "agent-window", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	// More than the window, each one identifiable. `Register` already wrote
	// one, so these are on top of it.
	const wrote = 25
	for i := 0; i < wrote; i++ {
		if err := s.AppendEvent(task.ID, EventNotified, map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}

	const window = 5
	events, err := s.Events(task.ID, window)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != window {
		t.Fatalf("asked for %d events, got %d", window, len(events))
	}

	// The newest five are n=20..24. Anything containing n=0 means the query is
	// still taking the oldest end.
	var got []float64
	for _, e := range events {
		var p struct {
			N float64 `json:"n"`
		}
		if err := json.Unmarshal(e.Payload, &p); err != nil {
			t.Fatalf("event payload %s: %v", e.Payload, err)
		}
		got = append(got, p.N)
	}
	for i, want := range []float64{20, 21, 22, 23, 24} {
		if got[i] != want {
			t.Fatalf("events are %v, want the newest five in time order [20 21 22 23 24]", got)
		}
	}

	// And ascending by time, which is what the timeline renders against.
	for i := 1; i < len(events); i++ {
		if events[i].At.Before(events[i-1].At) {
			t.Fatalf("event %d is older than the one before it: %s then %s",
				i, events[i-1].At, events[i].At)
		}
	}
}

func TestHaltRefusesFurtherWork(t *testing.T) {
	s := open(t)
	var gotCause error
	s.OnHalt = func(cause error) { gotCause = cause }

	// Close the underlying database to force a non-contention failure.
	if err := s.db.Close(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(Observed{WireName: "x", Worktree: "d:/x", Runner: "claude"}); err == nil {
		t.Fatal("expected an error once the database is unusable")
	}
	halted, cause := s.Halted()
	if !halted {
		t.Fatal("store did not halt on a hard failure")
	}
	if cause == nil || gotCause == nil {
		t.Fatalf("halt cause not reported: cause=%v callback=%v", cause, gotCause)
	}
	// Every later call must refuse rather than retry.
	if _, err := s.List(); err == nil {
		t.Fatal("halted store still served a read")
	}
}

// Priority: three levels, a date, and what it refuses.
//
// The only judgement on this board. Everything else stored about a card is a
// fact, which is why this one is the operator's alone.

func TestPriorityRoundTripsAndDatesItself(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "prio", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	// A new card carries no judgement. Empty is normal, and normal is what
	// almost everything should be.
	if task.Priority != "" || task.PriorityAt != nil {
		t.Fatalf("a new card came with priority %q at %v", task.Priority, task.PriorityAt)
	}

	if err := s.SetPriority(task.ID, PriorityHigh); err != nil {
		t.Fatal(err)
	}
	back, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Priority != PriorityHigh {
		t.Fatalf("priority came back %q", back.Priority)
	}
	// THE DATE IS THE POINT. Without it the board cannot tell a card marked
	// high this morning from one marked high a month ago and forgotten, and a
	// priority that never goes stale becomes a field where everything is high.
	if back.PriorityAt == nil {
		t.Fatal("setting a priority recorded no date, so nothing can fade it")
	}
}

// Clearing takes the date with it. A normal card has no judgement on it to be
// stale, and leaving the date behind would fade something that is not there.
func TestClearingPriorityClearsItsDate(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "prio-clear", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriority(task.ID, PriorityHigh); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPriority(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	back, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Priority != "" || back.PriorityAt != nil {
		t.Fatalf("cleared to %q at %v, want empty and no date", back.Priority, back.PriorityAt)
	}
}

// Anything that is not a level is refused rather than stored. The column has no
// CHECK constraint, on purpose, so this is the whole guard.
func TestPriorityRefusesAnythingElse(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "prio-bad", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{"urgent", "1", "HIGHEST", "p0", "medium"} {
		if err := s.SetPriority(task.ID, bad); err == nil {
			t.Fatalf("%q was accepted as a priority", bad)
		}
	}
	back, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Priority != "" {
		t.Fatalf("a refused value still landed: %q", back.Priority)
	}
}

// `normal` spelled out means empty. A caller clearing a priority reasonably
// sends either, and storing the word would make two values mean one thing.
func TestPriorityAcceptsTheWordNormalAsEmpty(t *testing.T) {
	for _, in := range []string{"normal", "NORMAL", "  Normal  ", ""} {
		got, ok := ValidPriority(in)
		if !ok || got != "" {
			t.Fatalf("ValidPriority(%q) = %q, %v; want empty and ok", in, got, ok)
		}
	}
	if got, ok := ValidPriority("  HIGH "); !ok || got != PriorityHigh {
		t.Fatalf("ValidPriority(\"  HIGH \") = %q, %v", got, ok)
	}
}

// Filing a card is not the session doing anything. Bumping activity would move
// a silent card to the top of an activity-sorted list, which is the same rule
// SetTags follows and for the same reason.
func TestSettingPriorityDoesNotLookLikeActivity(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "prio-quiet", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	before := task.LastActivityAt

	if err := s.SetPriority(task.ID, PriorityLow); err != nil {
		t.Fatal(err)
	}
	back, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !back.LastActivityAt.Equal(before) {
		t.Fatalf("last_activity_at moved from %s to %s. filing a card is not the session "+
			"doing something", before, back.LastActivityAt)
	}
}
