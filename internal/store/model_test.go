package store

import "testing"

// The two halves of choosing a model, in the store: what a runner can be asked
// for, and what a card was asked for.

func openTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(t.TempDir() + "/atrium.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// A runner's model arguments survive a round trip. They are a list held as
// JSON, exactly like the resume and prompt arguments beside them, and a column
// read back into the wrong field is not a compile error.
func TestModelArgumentsSurviveBeingSaved(t *testing.T) {
	s := openTestStore(t)

	saved, err := s.SaveHarness(Harness{
		ID: "mine", Label: "mine", Cmd: "claude", LaunchMode: LaunchPTY,
		ModelArgs: []string{"--model", "{model}"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(saved.ModelArgs) != 2 || saved.ModelArgs[1] != "{model}" {
		t.Fatalf("model arguments came back as %q", saved.ModelArgs)
	}

	// And again from a fresh read, not from what Save happened to return.
	got, err := s.Harness("mine")
	if err != nil {
		t.Fatal(err)
	}
	if len(got.ModelArgs) != 2 || got.ModelArgs[0] != "--model" {
		t.Fatalf("model arguments read back as %q", got.ModelArgs)
	}
}

// THE BACKFILL IS THE REASON THIS FEATURE WORKS ON A DATABASE THAT ALREADY
// EXISTS. `DefaultHarnesses` is seeded once on first run, so without it the
// operator's own `claude` row would gain the column and no way to use it, and
// the runner most likely to be asked for a model would be the one that refuses.
func TestAnExistingClaudeRowCanBeGivenAModel(t *testing.T) {
	s := openTestStore(t)

	h, err := s.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ModelArgs) == 0 {
		t.Fatal("the claude row cannot be given a model, so choosing one is refused " +
			"on every database that existed before this")
	}
	var carries bool
	for _, a := range h.ModelArgs {
		if a == "{model}" {
			carries = true
		}
	}
	if !carries {
		t.Fatalf("the claude row's model arguments never use the name: %q", h.ModelArgs)
	}
}

// A shell is left alone. It has no model and would try to execute the flag, so
// its row must stay empty and a launch naming a model must keep being refused.
func TestAShellIsNotGivenAModel(t *testing.T) {
	s := openTestStore(t)

	h, err := s.Harness("shell")
	if err != nil {
		t.Fatal(err)
	}
	if len(h.ModelArgs) != 0 {
		t.Fatalf("a shell was given model arguments: %q", h.ModelArgs)
	}
}

// What a card was launched on is durable, because `reopen` rebuilds a launch
// out of the card after a restart and a model not written down is a session
// moved back to the default.
func TestACardRemembersItsModel(t *testing.T) {
	s := openTestStore(t)

	task, _, err := s.Register(Observed{WireName: "one", Worktree: "/tmp/one"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetModel(task.ID, "  claude-fable-5-1  "); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	// Trimmed, because the box it came from is free text and a trailing space
	// would be a second model as far as the list below is concerned.
	if got.Model != "claude-fable-5-1" {
		t.Fatalf("the card remembers %q", got.Model)
	}
}

// ModelsUsed is a history of what has been typed, offered so the second launch
// on a model is not a second act of typing. It is never a catalog: atrium does
// not know which models exist.
func TestModelsUsedListsWhatWasChosen(t *testing.T) {
	s := openTestStore(t)

	for i, name := range []string{"claude-opus-5", "claude-fable-5-1", "claude-opus-5"} {
		task, _, err := s.Register(Observed{
			WireName: string(rune('a'+i)) + "-card", Worktree: "/tmp/x",
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.SetModel(task.ID, name); err != nil {
			t.Fatal(err)
		}
	}
	// A card with no model at all, which must not appear as an empty entry.
	if _, _, err := s.Register(Observed{WireName: "plain", Worktree: "/tmp/x"}); err != nil {
		t.Fatal(err)
	}

	got, err := s.ModelsUsed()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected two distinct models, got %q", got)
	}
	for _, name := range got {
		if name == "" {
			t.Fatalf("an empty model reached the list: %q", got)
		}
	}
}
