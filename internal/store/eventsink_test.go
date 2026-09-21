package store

import (
	"errors"
	"testing"
)

// The default, proven: with no event_sink setting the hot sink is the db table
// and there are no cold sinks. This is the whole promise that a fresh install
// and every existing one behave exactly as before.
func TestDefaultEventSinkIsDbAlone(t *testing.T) {
	s := open(t)
	if _, ok := s.hot.(*dbSink); !ok {
		t.Fatalf("default hot sink is %T, want *dbSink", s.hot)
	}
	if len(s.cold) != 0 {
		t.Fatalf("default install has %d cold sinks, want none", len(s.cold))
	}
}

// AppendEvent and Events round-trip through the sink exactly as the table did:
// newest limit, oldest first within the window.
func TestEventsRoundTripThroughHotSink(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "sink-rt", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	// Register already wrote a `created` event. Add a couple more.
	if err := s.AppendEvent(task.ID, EventPrompted, map[string]any{"text": "one"}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventPrompted, map[string]any{"text": "two"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("expected created + two prompts, got %d", len(got))
	}
	if got[0].Kind != EventCreated {
		t.Fatalf("oldest first broken: first kind is %q", got[0].Kind)
	}
	if got[len(got)-1].Kind != EventPrompted {
		t.Fatalf("newest last broken: last kind is %q", got[len(got)-1].Kind)
	}
}

// The limit applies to the NEWEST end. On a card with more events than the
// window, the window is the most recent ones, still oldest-first.
func TestEventsLimitKeepsNewest(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "sink-window", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 10; i++ {
		if err := s.AppendEvent(task.ID, EventPrompted, map[string]any{"n": i}); err != nil {
			t.Fatal(err)
		}
	}
	got, err := s.Events(task.ID, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("window of three returned %d", len(got))
	}
	// The last three prompts are n=7,8,9. Oldest-first means the window opens on
	// the older of those, not on the created event at the very start.
	if got[0].Kind != EventPrompted {
		t.Fatalf("window reached past the newest three: %q", got[0].Kind)
	}
}

// A cold sink never fails the caller. AppendEvent must return nil even when a
// cold sink's Append reports trouble, because a hook must never fail a session.
func TestColdSinkFailureNeverFailsAppend(t *testing.T) {
	s := open(t)
	s.cold = append(s.cold, failCold{})
	task, _, err := s.Register(Observed{WireName: "cold-fail", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventPrompted, nil); err != nil {
		t.Fatalf("a cold sink's failure reached the caller: %v", err)
	}
	// The hot sink still recorded it: created plus the prompt.
	got, err := s.Events(task.ID, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("hot sink lost an event when a cold sink failed: got %d", len(got))
	}
}

// failCold is a cold sink whose Append always errors, standing in for a broken
// or backed-up durability target.
type failCold struct{}

func (failCold) Append(taskID string, e *Event) error { return errAlwaysFails }
func (failCold) Recent(taskID string, limit int) ([]*Event, error) {
	return nil, ErrRecentUnsupported
}

var errAlwaysFails = errors.New("cold sink is down")
