package store

import (
	"database/sql"
	"errors"
	"testing"
)

// Where a card's work came from, and the rules that make it deduplicable.
//
// See docs/intake-design.md. The column existed for a long time and was
// written by nothing, so none of this had anywhere to be tested before.

func TestSetOriginRecordsAllThree(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "a", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetOrigin(task.ID, "github", "openziti/ziti#4211",
		"https://github.com/openziti/ziti/issues/4211"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "github" {
		t.Fatalf("source is %q", got.Source)
	}
	if got.ExternalID != "openziti/ziti#4211" {
		t.Fatalf("external id is %q", got.ExternalID)
	}
	if got.URL == "" {
		t.Fatal("the way back to the item was not recorded")
	}
}

// A caller that knows only one of the three must not blank out what another
// already supplied. A poller knows the identifier; a human pasting a link
// knows the url; neither should erase the other.
func TestSetOriginLeavesWhatItIsNotTold(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "b", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(task.ID, "zendesk", "12345", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(task.ID, "", "", "https://example.zendesk.com/agent/tickets/12345"); err != nil {
		t.Fatal(err)
	}

	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Source != "zendesk" || got.ExternalID != "12345" {
		t.Fatalf("a later call blanked what an earlier one set: %q %q", got.Source, got.ExternalID)
	}
	if got.URL == "" {
		t.Fatal("the url was not written")
	}
}

// The pair is the key. The same number is an issue in one tracker and a ticket
// in another, and they are not the same work.
func TestBySourceExternalIsKeyedOnThePair(t *testing.T) {
	s := open(t)
	issue, _, err := s.Register(Observed{WireName: "issue", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	ticket, _, err := s.Register(Observed{WireName: "ticket", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(issue.ID, "github", "4211", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(ticket.ID, "zendesk", "4211", ""); err != nil {
		t.Fatal(err)
	}

	gotIssue, err := s.BySourceExternal("github", "4211")
	if err != nil {
		t.Fatal(err)
	}
	gotTicket, err := s.BySourceExternal("zendesk", "4211")
	if err != nil {
		t.Fatal(err)
	}
	if gotIssue.ID == gotTicket.ID {
		t.Fatal("two trackers sharing an identifier collapsed into one card")
	}
	if gotIssue.ID != issue.ID || gotTicket.ID != ticket.ID {
		t.Fatal("the pair did not resolve to the card that carries it")
	}
}

// Half a key is not a key. Answering with some arbitrary card that happens to
// carry that identifier would make a poller adopt unrelated work.
func TestBySourceExternalRefusesHalfAKey(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "c", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(task.ID, "github", "4211", ""); err != nil {
		t.Fatal(err)
	}

	for _, tc := range [][2]string{{"", "4211"}, {"github", ""}, {"", ""}} {
		if _, err := s.BySourceExternal(tc[0], tc[1]); !errors.Is(err, sql.ErrNoRows) {
			t.Fatalf("source=%q external=%q answered with %v, wanted no rows", tc[0], tc[1], err)
		}
	}
}

// A card raised, worked and swept must not come back the next time the source
// that raised it runs. Archiving takes a card off the board, and off the board
// is not the same as never having happened.
func TestBySourceExternalStillFindsAnArchivedCard(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "d", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(task.ID, "github", "4211", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDead); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Archive(0, StatusDead); err != nil {
		t.Fatal(err)
	}

	got, err := s.BySourceExternal("github", "4211")
	if err != nil {
		t.Fatalf("an archived card became invisible to deduplication: %v", err)
	}
	if got.ID != task.ID {
		t.Fatal("found the wrong card")
	}
	if got.ArchivedAt == nil {
		t.Fatal("the card under test was not actually archived")
	}
}

// The seeded runners that can take an opening prompt say so, and the one that
// would try to execute it does not.
func TestPromptArgsAreSeededOnlyWhereTheyWork(t *testing.T) {
	s := open(t)

	claude, err := s.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(claude.PromptArgs) == 0 {
		t.Fatal("claude cannot be handed an opening prompt")
	}
	if claude.PromptArgs[0] != "{prompt}" {
		t.Fatalf("claude takes a prompt as %q", claude.PromptArgs[0])
	}

	shell, err := s.Harness("shell")
	if err != nil {
		t.Fatal(err)
	}
	if len(shell.PromptArgs) != 0 {
		t.Fatal("a shell claims to take a prompt, which it would try to execute")
	}
}

// The field survives a round trip through SaveHarness, which rewrites every
// column and is where a forgotten one goes missing.
func TestPromptArgsSurviveASave(t *testing.T) {
	s := open(t)
	if _, err := s.SaveHarness(Harness{
		ID: "custom", Cmd: "thing", LaunchMode: LaunchPTY,
		PromptArgs: []string{"--task", "{prompt}"},
	}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Harness("custom")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.PromptArgs) != 2 || got.PromptArgs[1] != "{prompt}" {
		t.Fatalf("prompt args came back as %v", got.PromptArgs)
	}
}
