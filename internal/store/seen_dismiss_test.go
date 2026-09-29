package store

import "testing"

// Dismissing answers the questions and nothing else: the turn stays unseen,
// because a dismissed question is not a read turn.
func TestDismissQuestionsAnswersAndLeavesSeen(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true, List: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	shown := *seenOf(t, s, task.ID).QuestionsAt

	dismissed, stale, err := s.DismissQuestions(task.ID, shown)
	if err != nil || !dismissed || stale {
		t.Fatalf("dismissed=%v stale=%v err=%v", dismissed, stale, err)
	}
	got := seenOf(t, s, task.ID)
	v := got.View()
	if v.Answered == nil || !*v.Answered || v.AnsweredVia != SeenDismissed || len(v.OpenQuestions) != 0 {
		t.Fatalf("after dismissing: %+v", v)
	}
	if !got.Unseen() || got.SeenAt != nil {
		t.Fatalf("dismissing marked the turn seen: %+v", got)
	}

	// Nothing open is not an error, and not stale.
	dismissed, stale, err = s.DismissQuestions(task.ID, shown)
	if err != nil || dismissed || stale {
		t.Fatalf("second dismiss: dismissed=%v stale=%v err=%v", dismissed, stale, err)
	}
	// A card with no row at all likewise.
	other := askCard(t, s, "other")
	if dismissed, stale, err = s.DismissQuestions(other.ID, shown); err != nil || dismissed || stale {
		t.Fatalf("no row: dismissed=%v stale=%v err=%v", dismissed, stale, err)
	}
}

// A chip drawn before newer questions arrived names an older set, and must not
// dismiss the newer one.
func TestDismissQuestionsRefusesAnOlderShownSet(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true, List: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	old := *seenOf(t, s, task.ID).QuestionsAt
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true, List: []string{"b"}}); err != nil {
		t.Fatal(err)
	}

	dismissed, stale, err := s.DismissQuestions(task.ID, old)
	if err != nil || dismissed || !stale {
		t.Fatalf("dismissed=%v stale=%v err=%v", dismissed, stale, err)
	}
	if got := seenOf(t, s, task.ID); got.AnsweredAt != nil || got.View().OpenCount() != 1 {
		t.Fatalf("a stale dismiss changed the card: %+v", got)
	}
}
