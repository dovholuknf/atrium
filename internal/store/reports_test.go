package store

import (
	"testing"
)

// Reading back what a session said about itself.

func reportCard(t *testing.T, s *Store, name string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{
		WireName: name, Worktree: "d:/git/atrium/" + name, Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// The newest report wins. A session that asked a question and then finished
// has finished, and a list that showed the question would send somebody to
// answer work that is already over.
func TestOnlyTheNewestReportPerCardIsReturned(t *testing.T) {
	s := open(t)
	task := reportCard(t, s, "twice")

	if err := s.AppendEvent(task.ID, EventSubmitted, map[string]any{
		"by": "agent", "kind": ReportAsked, "ask": "which schema", "blocked": true,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(task.ID, EventSubmitted, map[string]any{
		"by": "agent", "kind": ReportFinished,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LatestAgentReports()
	if err != nil {
		t.Fatal(err)
	}
	if got[task.ID].Kind != ReportFinished {
		t.Fatalf("the older report won: %+v", got[task.ID])
	}
}

// THE REFUSAL. `submitted` carries more than these two things, and a payload
// this does not recognise is an event it does not know about rather than an
// error. Reading one as a report would put words on a card that nobody said.
func TestSomethingThatIsNotAnAgentReportIsIgnored(t *testing.T) {
	s := open(t)
	task := reportCard(t, s, "noise")

	// The hub's own submit event: no `by`, and a kind that means something
	// else entirely.
	if err := s.AppendEvent(task.ID, EventSubmitted, map[string]any{
		"kind": "greeting", "content": "hello",
	}); err != nil {
		t.Fatal(err)
	}
	// A human moving a card is not an agent reporting either.
	if err := s.AppendEvent(task.ID, EventSubmitted, map[string]any{
		"by": "human", "kind": ReportFinished,
	}); err != nil {
		t.Fatal(err)
	}
	// And a payload that is not the shape at all.
	if err := s.AppendEvent(task.ID, EventSubmitted, "not an object"); err != nil {
		t.Fatal(err)
	}

	got, err := s.LatestAgentReports()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := got[task.ID]; ok {
		t.Fatalf("invented a report from something else: %+v", got[task.ID])
	}
}

// Blocked and still-working are two facts about one moment, and both come back
// off the timeline. Nothing else records the difference: a session that asks
// while it carries on does not move its card on purpose, so no column changed.
func TestAWorkingAskKeepsItsWordsAndItsBlockedFlag(t *testing.T) {
	s := open(t)
	task := reportCard(t, s, "working-ask")

	if err := s.AppendEvent(task.ID, EventSubmitted, map[string]any{
		"by": "agent", "kind": ReportAsked,
		"ask": "do you want the postgres path stubbed", "blocked": false,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := s.LatestAgentReports()
	if err != nil {
		t.Fatal(err)
	}
	rep := got[task.ID]
	if rep.Kind != ReportAsked || rep.Blocked {
		t.Fatalf("a working ask came back as %+v", rep)
	}
	if rep.Ask != "do you want the postgres path stubbed" {
		t.Fatalf("the words did not survive: %q", rep.Ask)
	}
	if rep.At.IsZero() {
		t.Fatal("no time on the report, so nothing can order by it")
	}
}
