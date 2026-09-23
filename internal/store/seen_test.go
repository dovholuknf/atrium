package store

import (
	"testing"
	"time"
)

// The defect these are written against: the orchestrator ended turns with an
// Open Questions block that nobody read, and kept saying the questions were
// still open. Neither side could tell a turn that was read from one that was
// not.

// stepClock moves `now` forward by a second on every call, so the order of
// events is the order of timestamps and nothing depends on the wall clock.
func stepClock(t *testing.T) {
	t.Helper()
	base := time.Date(2026, 9, 23, 12, 0, 0, 0, time.UTC)
	prev := now
	n := 0
	now = func() time.Time { n++; return base.Add(time.Duration(n) * time.Second) }
	t.Cleanup(func() { now = prev })
}

func seenOf(t *testing.T, s *Store, id string) *Seen {
	t.Helper()
	got, err := s.GetSeen(id)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func TestNoTurnMeansNothingToSee(t *testing.T) {
	s := open(t)
	task := askCard(t, s, "fresh")
	if got := seenOf(t, s, task.ID); got != nil {
		t.Fatalf("a card no turn ended on has a seen row: %+v", got)
	}
	// Answering or seeing a card with no turn creates nothing.
	if changed, err := s.MarkAnswered(task.ID, SeenPrompt); err != nil || changed {
		t.Fatalf("answered a card with no turn: changed=%v err=%v", changed, err)
	}
	if changed, err := s.MarkSeen(task.ID, SeenViewed, nil); err != nil || changed {
		t.Fatalf("saw a card with no turn: changed=%v err=%v", changed, err)
	}
	if got := seenOf(t, s, task.ID); got != nil {
		t.Fatal("marking a card with no turn created a row")
	}
}

func TestATurnEndingIsUnseenUntilSeen(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")

	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true}); err != nil {
		t.Fatal(err)
	}
	if !seenOf(t, s, task.ID).Unseen() {
		t.Fatal("a turn that just ended reads as seen")
	}
	changed, err := s.MarkSeen(task.ID, SeenViewed, nil)
	if err != nil || !changed {
		t.Fatalf("seeing it changed nothing: %v %v", changed, err)
	}
	got := seenOf(t, s, task.ID)
	if got.Unseen() || got.SeenVia != SeenViewed {
		t.Fatalf("after seeing: unseen=%v via=%q", got.Unseen(), got.SeenVia)
	}
	// Seeing it twice is not a change, so nothing is republished.
	if changed, _ := s.MarkSeen(task.ID, SeenViewed, nil); changed {
		t.Fatal("seeing a seen turn reported a change")
	}
	// The next turn is unseen again.
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true}); err != nil {
		t.Fatal(err)
	}
	if !seenOf(t, s, task.ID).Unseen() {
		t.Fatal("a second turn inherited the first one's seen")
	}
}

// A board that has not heard about the newest turn cannot mark it seen, since
// what it was showing was the turn before.
func TestAStaleBoardDoesNotSeeANewerTurn(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true}); err != nil {
		t.Fatal(err)
	}
	shown := *seenOf(t, s, task.ID).TurnEndedAt
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true}); err != nil {
		t.Fatal(err)
	}
	changed, err := s.MarkSeen(task.ID, SeenViewed, &shown)
	if err != nil {
		t.Fatal(err)
	}
	if changed || !seenOf(t, s, task.ID).Unseen() {
		t.Fatal("a board showing the older turn marked the newer one seen")
	}
	newest := *seenOf(t, s, task.ID).TurnEndedAt
	if changed, _ := s.MarkSeen(task.ID, SeenViewed, &newest); !changed {
		t.Fatal("a board showing the newest turn could not mark it seen")
	}
}

func TestQuestionsReplaceKeepAndAnswer(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")

	four := TurnQuestions{Known: true, Block: true, List: []string{"a", "b", "c", "d"}}
	if err := s.NoteTurnEnded(task.ID, four); err != nil {
		t.Fatal(err)
	}
	v := seenOf(t, s, task.ID).View()
	if v.OpenCount() != 4 || v.Answered == nil || *v.Answered {
		t.Fatalf("four questions asked: %+v", v)
	}

	// A turn with no block, as when a peer reported in, keeps them.
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true}); err != nil {
		t.Fatal(err)
	}
	if n := seenOf(t, s, task.ID).View().OpenCount(); n != 4 {
		t.Fatalf("a turn with no block left %d questions, want the 4 still owed", n)
	}
	// A turn whose text could not be read keeps them too, even if it claims a
	// block, because nothing it says was read.
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: false, Block: true}); err != nil {
		t.Fatal(err)
	}
	if n := seenOf(t, s, task.ID).View().OpenCount(); n != 4 {
		t.Fatalf("an unreadable turn left %d questions", n)
	}

	// A new block replaces.
	two := TurnQuestions{Known: true, Block: true, List: []string{"which branch", "  ", "which base"}}
	if err := s.NoteTurnEnded(task.ID, two); err != nil {
		t.Fatal(err)
	}
	v = seenOf(t, s, task.ID).View()
	if len(v.OpenQuestions) != 2 || v.OpenQuestions[0] != "which branch" {
		t.Fatalf("a new block did not replace the old one: %+v", v.OpenQuestions)
	}

	// The operator replies: answered and seen at once.
	changed, err := s.MarkAnswered(task.ID, SeenPrompt)
	if err != nil || !changed {
		t.Fatalf("answering changed nothing: %v %v", changed, err)
	}
	v = seenOf(t, s, task.ID).View()
	if v.Answered == nil || !*v.Answered || len(v.OpenQuestions) != 0 || v.Unseen {
		t.Fatalf("after answering: %+v", v)
	}

	// A later turn with a block asks again, and the earlier reply does not
	// answer it.
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true, List: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if v := seenOf(t, s, task.ID).View(); v.OpenCount() != 1 {
		t.Fatalf("a reply before the question answered it: %+v", v)
	}
}

func TestAHeadingWithNoReadableItemsIsStillAsked(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true}); err != nil {
		t.Fatal(err)
	}
	v := seenOf(t, s, task.ID).View()
	if v.OpenCount() != -1 || !v.QuestionsUnparsed {
		t.Fatalf("a heading with no items is not recorded as asked: %+v", v)
	}
}

func TestQuestionsAreBounded(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")
	long := make([]byte, MaxAsk*3)
	for i := range long {
		long[i] = 'q'
	}
	var many []string
	for i := 0; i < MaxTurnQuestions+5; i++ {
		many = append(many, string(long))
	}
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true, List: many}); err != nil {
		t.Fatal(err)
	}
	got := seenOf(t, s, task.ID)
	if len(got.Questions) != MaxTurnQuestions {
		t.Fatalf("kept %d questions, want %d", len(got.Questions), MaxTurnQuestions)
	}
	for _, q := range got.Questions {
		if len(q) > MaxAsk+8 {
			t.Fatalf("a question of %d bytes was kept", len(q))
		}
	}
}

func TestSeenIsListedAndGoesWithTheCard(t *testing.T) {
	stepClock(t)
	s := open(t)
	task := askCard(t, s, "orchestrator")
	if err := s.NoteTurnEnded(task.ID, TurnQuestions{Known: true, Block: true, List: []string{"a"}}); err != nil {
		t.Fatal(err)
	}
	all, err := s.SeenAll()
	if err != nil || all[task.ID] == nil || !all[task.ID].Unseen() {
		t.Fatalf("SeenAll does not carry the card: %v %+v", err, all)
	}
	ids, err := s.UnseenCards()
	if err != nil || len(ids) != 1 || ids[0] != task.ID {
		t.Fatalf("UnseenCards = %v %v", ids, err)
	}
	if err := s.Forget(task.ID); err != nil {
		t.Fatal(err)
	}
	if got := seenOf(t, s, task.ID); got != nil {
		t.Fatal("the seen row outlived its card")
	}
}

// The migration tolerates being run again, which is the rule for all of them.
func TestTurnSeenMigrationReruns(t *testing.T) {
	s := open(t)
	if _, err := s.db.Exec(`DELETE FROM schema_migration WHERE name = '0057_turn_seen'`); err != nil {
		t.Fatal(err)
	}
	if err := s.migrate(); err != nil {
		t.Fatalf("re-running 0057 failed: %v", err)
	}
}
