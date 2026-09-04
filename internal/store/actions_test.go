package store

import (
	"strings"
	"testing"
)

// Actions on a card, and the rules about which card they belong on.

func TestTheSeededActionsAreThereOnce(t *testing.T) {
	path := t.TempDir() + "/atrium.db"
	first, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	actions, err := first.CardActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(actions) != len(DefaultCardActions()) {
		t.Fatalf("a fresh database has %d actions", len(actions))
	}
	// The one that makes `atrium finish` reachable at all. Nothing else tells
	// a session that command exists.
	found := false
	for _, a := range actions {
		if strings.Contains(a.Prompt, "atrium finish") {
			found = true
		}
	}
	if !found {
		t.Fatal("nothing seeded tells a session how to say it finished")
	}

	// Deleting them all must not bring them back on the next open, or an
	// operator who does not want them has no way to say so.
	for _, a := range actions {
		if err := first.DeleteCardAction(a.ID); err != nil {
			t.Fatal(err)
		}
	}
	first.Close()

	second, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	again, err := second.CardActions()
	if err != nil {
		t.Fatal(err)
	}
	if len(again) != 0 {
		t.Fatalf("deleted actions came back on reopen: %d", len(again))
	}
}

func TestAnActionNeedsANameAndSomethingToSay(t *testing.T) {
	s := open(t)
	if _, err := s.SaveCardAction(CardAction{Prompt: "do it"}); err == nil {
		t.Fatal("an action with no name was accepted")
	}
	if _, err := s.SaveCardAction(CardAction{Label: "do it"}); err == nil {
		t.Fatal("an action with nothing to say was accepted")
	}
	if _, err := s.SaveCardAction(CardAction{
		Label: "essay", Prompt: strings.Repeat("x", MaxActionPrompt+1),
	}); err == nil {
		t.Fatal("an action longer than an instruction was accepted")
	}
}

// An unrecognized `after` is `keep`, which is the harmless one. Guessing exit
// would quit a session because a value was mistyped.
func TestAnUnknownAfterIsKeep(t *testing.T) {
	s := open(t)
	got, err := s.SaveCardAction(CardAction{
		Label: "x", Prompt: "y", After: "explode", Enabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.After != AfterKeep {
		t.Fatalf("after came back as %q", got.After)
	}
}

func TestAnActionWithNoConditionsIsOfferedEverywhere(t *testing.T) {
	a := &CardAction{Label: "x", Prompt: "y", Enabled: true}
	for _, task := range []*Task{
		{Runner: "claude", Tags: []string{}},
		{Runner: "codex", Tags: []string{"lab"}},
		{Runner: "", Tags: nil},
	} {
		if !a.Offers(task) {
			t.Fatalf("an unconditional action was withheld from %+v", task)
		}
	}
}

// "Run the tests" means something different in a Go repo and a docs repo, and
// a tag is the cut the operator already makes.
func TestATagLimitsAnActionToCardsCarryingIt(t *testing.T) {
	a := &CardAction{Label: "x", Prompt: "y", Enabled: true, Tag: "go"}
	if !a.Offers(&Task{Tags: []string{"lab", "go"}}) {
		t.Fatal("an action was withheld from a card carrying its tag")
	}
	if a.Offers(&Task{Tags: []string{"docs"}}) {
		t.Fatal("a tagged action was offered on a card without that tag")
	}
	if a.Offers(&Task{Tags: nil}) {
		t.Fatal("a tagged action was offered on an untagged card")
	}
	// Tags are lower cased on the way in, and matching is not case sensitive,
	// so `Go` and `go` stay one tag here as they do everywhere else.
	if !a.Offers(&Task{Tags: []string{"GO"}}) {
		t.Fatal("tag matching is case sensitive")
	}
}

func TestARunnerLimitsAnActionToThatHarness(t *testing.T) {
	a := &CardAction{Label: "x", Prompt: "y", Enabled: true, Runner: "claude"}
	if !a.Offers(&Task{Runner: "claude"}) {
		t.Fatal("an action was withheld from its own runner")
	}
	if a.Offers(&Task{Runner: "codex"}) {
		t.Fatal("a runner-specific action was offered on another runner")
	}
	// A card with no runner has no session, and an action is a claim about
	// what a session can do.
	if a.Offers(&Task{Runner: ""}) {
		t.Fatal("a runner-specific action was offered on a card with no runner")
	}
}

// Both conditions have to pass. Either one alone would make the other
// decorative.
func TestBothConditionsApply(t *testing.T) {
	a := &CardAction{Label: "x", Prompt: "y", Enabled: true, Tag: "go", Runner: "claude"}
	if !a.Offers(&Task{Runner: "claude", Tags: []string{"go"}}) {
		t.Fatal("an action matching both conditions was withheld")
	}
	if a.Offers(&Task{Runner: "claude", Tags: []string{"docs"}}) {
		t.Fatal("the tag condition was ignored")
	}
	if a.Offers(&Task{Runner: "codex", Tags: []string{"go"}}) {
		t.Fatal("the runner condition was ignored")
	}
}

func TestADisabledActionIsOfferedNowhere(t *testing.T) {
	a := &CardAction{Label: "x", Prompt: "y", Enabled: false}
	if a.Offers(&Task{Runner: "claude"}) {
		t.Fatal("a disabled action was offered")
	}
}

func TestActionsForFiltersTheStoredSet(t *testing.T) {
	s := open(t)
	for _, a := range []CardAction{
		{ID: "any", Label: "any", Prompt: "p", Enabled: true, Sort: 1},
		{ID: "go-only", Label: "go", Prompt: "p", Enabled: true, Tag: "go", Sort: 2},
		{ID: "off", Label: "off", Prompt: "p", Enabled: false, Sort: 3},
	} {
		if _, err := s.SaveCardAction(a); err != nil {
			t.Fatal(err)
		}
	}
	// The seeded ones are enabled and unconditional, so count only the ones
	// under test by id.
	got, err := s.ActionsFor(&Task{Runner: "claude", Tags: []string{"docs"}})
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, a := range got {
		seen[a.ID] = true
	}
	if !seen["any"] {
		t.Fatal("an unconditional action was not offered")
	}
	if seen["go-only"] {
		t.Fatal("a tagged action was offered on a card without that tag")
	}
	if seen["off"] {
		t.Fatal("a disabled action was offered")
	}
}

// Saving without an id makes one. A caller that had to mint one would be
// inventing a key format atrium owns.
func TestSavingWithoutAnIDMakesOne(t *testing.T) {
	s := open(t)
	got, err := s.SaveCardAction(CardAction{Label: "new one", Prompt: "hello", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	if got.ID == "" {
		t.Fatal("no id was minted")
	}
	// And saving it again keeps the id and the creation time rather than
	// making a second row.
	again, err := s.SaveCardAction(*got)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != got.ID {
		t.Fatal("saving an existing action made a new one")
	}
	if !again.CreatedAt.Equal(got.CreatedAt) {
		t.Fatal("saving an existing action moved its creation time")
	}
}
