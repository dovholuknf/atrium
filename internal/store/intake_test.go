package store

import (
	"strings"
	"testing"
)

// The inbox. See docs/intake-design.md, and the round one review notes in
// .mercurius/s_yrt82p4nug6O/round-01/, which is where three of these tests
// come from.

func item(source, id string) IntakeItem {
	return IntakeItem{
		Source: source, ExternalID: id,
		Title: "tunneler drops DNS on resume",
		URL:   "https://example.invalid/" + id,
	}
}

func TestOfferCreatesACardWithNoRunner(t *testing.T) {
	s := open(t)

	task, created, err := s.Offer(item("github", "openziti/ziti#4211"))
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("the first offer did not create a card")
	}
	if task.Status != StatusBacklog {
		t.Fatalf("an offered card landed in %q", task.Status)
	}
	if task.WireName != "" {
		t.Fatalf("an offered card claimed a wire name: %q", task.WireName)
	}
	if task.PID != 0 {
		t.Fatal("an offered card claimed a process")
	}
	if task.Source != "github" || task.ExternalID != "openziti/ziti#4211" {
		t.Fatalf("the origin was not recorded: %q %q", task.Source, task.ExternalID)
	}
}

// A source posts everything it can see on every tick and lets atrium work out
// what is new. Anything else means every source keeping its own memory.
func TestOfferingTheSameItemTwiceIsOneCard(t *testing.T) {
	s := open(t)

	first, created, err := s.Offer(item("github", "4211"))
	if err != nil || !created {
		t.Fatalf("first offer: created=%v err=%v", created, err)
	}
	second, created, err := s.Offer(item("github", "4211"))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("the second tick raised a second card for the same item")
	}
	if first.ID != second.ID {
		t.Fatal("the second tick answered with a different card")
	}
}

// Two scripts spelling the same system differently are one source. This is the
// normalization gap round one raised as C2.
func TestSourceCaseDoesNotSplitAnItem(t *testing.T) {
	s := open(t)

	first, _, err := s.Offer(item("GitHub", "4211"))
	if err != nil {
		t.Fatal(err)
	}
	second, created, err := s.Offer(item("  github  ", "4211"))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("GitHub and github raised two cards")
	}
	if first.ID != second.ID {
		t.Fatal("the two spellings landed on different cards")
	}
	if first.Source != "github" {
		t.Fatalf("the stored source was not canonicalized: %q", first.Source)
	}
}

// The identifier alone is not the key. 4211 is an issue in one tracker and a
// ticket in another.
func TestTheSameNumberInTwoTrackersIsTwoCards(t *testing.T) {
	s := open(t)

	issue, _, err := s.Offer(item("github", "4211"))
	if err != nil {
		t.Fatal(err)
	}
	ticket, created, err := s.Offer(item("zendesk", "4211"))
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("a zendesk ticket was swallowed by a github issue with the same number")
	}
	if issue.ID == ticket.ID {
		t.Fatal("two trackers collapsed onto one card")
	}
}

// Half a key is refused rather than stored, because a source producing either
// half would raise the same work again on every tick.
func TestAnItemWithoutBothHalvesOfAKeyIsRefused(t *testing.T) {
	s := open(t)

	for _, tc := range []struct {
		name           string
		source, extern string
	}{
		{"no source", "", "4211"},
		{"no identifier", "github", ""},
		{"neither", "", ""},
		{"whitespace only", "   ", "  "},
	} {
		if _, _, err := s.Offer(item(tc.source, tc.extern)); err == nil {
			t.Fatalf("%s was accepted", tc.name)
		}
	}
}

// Round one, C3. An offered card has no session, so nothing that answers for a
// session may answer for it. Today that holds because the wire name is empty
// and the pid is zero and three separate pieces of code all check one of those.
// This asserts the outcome rather than the argument.
func TestAnOfferedCardIsNotReachableAsASession(t *testing.T) {
	s := open(t)

	offered, _, err := s.Offer(item("zendesk", "12345"))
	if err != nil {
		t.Fatal(err)
	}

	// Not listed as wanting a human. It wants a human, but not in the sense
	// this list means, which is "an agent is frozen until you answer".
	waiting, err := s.Waiting()
	if err != nil {
		t.Fatal(err)
	}
	for _, w := range waiting {
		if w.ID == offered.ID {
			t.Fatal("an offered card appeared in the waiting list")
		}
	}

	// A runner announcing itself must not land on it. The card describes work,
	// not a process, and adopting it would give a session a card whose whole
	// history belongs to something else.
	got, created, err := s.Register(Observed{
		WireName: "something-else", Worktree: "d:/w", Runner: "claude", PID: 4242,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("a registering runner adopted a card instead of making one")
	}
	if got.ID == offered.ID {
		t.Fatal("a registering runner landed on an offered card")
	}

	// And it is not gated, so the permission chain has nothing to answer for.
	if offered.Gated {
		t.Fatal("an offered card is gated, so the chain would answer for a session it does not have")
	}
}

// A card raised, worked and swept must not come back the next time its source
// runs. This is the failure mode that makes a poller unusable.
func TestAnArchivedItemIsStillDeduplicated(t *testing.T) {
	s := open(t)

	first, _, err := s.Offer(item("ci", "run/991"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(first.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(0, StatusDone); err != nil {
		t.Fatal(err)
	}

	second, created, err := s.Offer(item("ci", "run/991"))
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("a swept item came back on the next tick")
	}
	if second.ID != first.ID {
		t.Fatal("the answer was a different card")
	}
}

// The inbox is what has not been started. A card that has been started is on
// the board doing something and does not belong in a list of suggestions.
func TestOfferedListsOnlyWhatHasNotStarted(t *testing.T) {
	s := open(t)

	waiting, _, err := s.Offer(item("github", "1"))
	if err != nil {
		t.Fatal(err)
	}
	started, _, err := s.Offer(item("github", "2"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(started.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}

	offered, err := s.Offered()
	if err != nil {
		t.Fatal(err)
	}
	if len(offered) != 1 || offered[0].ID != waiting.ID {
		t.Fatalf("the inbox holds %d cards, wanted only the unstarted one", len(offered))
	}
}

// Starting an offered card attaches a name to the card that already exists.
// Registering instead would make a second one and strand the first.
func TestClaimAttachesASessionToAnOfferedCard(t *testing.T) {
	s := open(t)

	offered, _, err := s.Offer(item("github", "4211"))
	if err != nil {
		t.Fatal(err)
	}

	claimed, err := s.Claim(offered.ID, Observed{
		WireName: "ziti-4211", Worktree: "d:/w/ziti", Runner: "claude", PID: 77,
	})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.ID != offered.ID {
		t.Fatal("claiming made a different card")
	}
	if claimed.WireName != "ziti-4211" {
		t.Fatalf("the card answers to %q", claimed.WireName)
	}

	// And it is now reachable the ordinary way, which is the whole point.
	back, err := s.GetByWireName("ziti-4211")
	if err != nil {
		t.Fatal(err)
	}
	if back.ID != offered.ID {
		t.Fatal("the claimed card is not resolvable by its name")
	}
	if back.ExternalID != "4211" {
		t.Fatal("the started session lost the link to what it was raised for")
	}
}

// Renaming a live session is refused. Every card the agent endpoints resolve
// is resolved by that name.
func TestClaimingATakenCardIsRefused(t *testing.T) {
	s := open(t)

	live, _, err := s.Register(Observed{WireName: "already-here", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Claim(live.ID, Observed{WireName: "renamed", Worktree: "d:/w", Runner: "claude"}); err == nil {
		t.Fatal("a live session was renamed by a claim")
	}
}

// A source's suggested directory stands until a runner reports a real one.
func TestClaimKeepsTheSuggestedDirectoryWhenTheRunnerHasNone(t *testing.T) {
	s := open(t)

	it := item("github", "4211")
	it.SuggestedCwd = "d:/worktrees/ziti/issue-4211"
	offered, _, err := s.Offer(it)
	if err != nil {
		t.Fatal(err)
	}
	if offered.Worktree != it.SuggestedCwd {
		t.Fatalf("the suggested directory was not kept: %q", offered.Worktree)
	}

	claimed, err := s.Claim(offered.ID, Observed{WireName: "n", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if claimed.Worktree != it.SuggestedCwd {
		t.Fatalf("claiming blanked the directory: %q", claimed.Worktree)
	}
}

// The instruction survives until somebody starts the card, and can be edited
// first, because a source's wording is a guess by something that never read
// the ticket.
func TestThePromptSurvivesUntilItIsStartedAndCanBeEdited(t *testing.T) {
	s := open(t)

	it := item("zendesk", "12345")
	it.Prompt = "read this ticket and summarize it"
	offered, _, err := s.Offer(it)
	if err != nil {
		t.Fatal(err)
	}
	if offered.Prompt != it.Prompt {
		t.Fatalf("the prompt was not stored: %q", offered.Prompt)
	}

	if err := s.SetPrompt(offered.ID, "read it, then tell me which repo this is about"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(offered.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.Prompt, "which repo") {
		t.Fatalf("the edited prompt did not stick: %q", got.Prompt)
	}
}

// The key cannot be forged by moving a separator across the join.
func TestIntakeKeyCannotCollideAcrossTheJoin(t *testing.T) {
	if IntakeKey("a", "b:c") == IntakeKey("a:b", "c") {
		t.Fatal("two different items produce the same key")
	}
	if IntakeKey("", "x") != "" || IntakeKey("x", "") != "" {
		t.Fatal("half a key produced a key")
	}
}
