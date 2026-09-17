package store

import "testing"

// Persist paste support in the harness so the board still knows about it
// after the startup enable sequence falls out of scrollback.

// The flag survives a round trip. It is stored as an integer and read back into
// a bool beside sixteen other columns, and a scan that lands in the wrong field
// is not a compile error.
func TestBracketedPasteSurvivesBeingSaved(t *testing.T) {
	s := openTestStore(t)

	saved, err := s.SaveHarness(Harness{
		ID: "mine", Label: "mine", Cmd: "claude", LaunchMode: LaunchPTY,
		BracketedPaste: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !saved.BracketedPaste {
		t.Fatal("a runner declared as asking for bracketed paste came back saying it does not")
	}

	got, err := s.Harness("mine")
	if err != nil {
		t.Fatal(err)
	}
	if !got.BracketedPaste {
		t.Fatal("the declaration did not survive a fresh read, so every paste into this " +
			"runner is delivered a line at a time")
	}

	// And off stays off, which is the side that keeps `200~` off the screen of
	// a runner that never asked for the mode.
	if _, err := s.SaveHarness(Harness{
		ID: "plain", Label: "plain", Cmd: "bash", LaunchMode: LaunchPTY,
	}); err != nil {
		t.Fatal(err)
	}
	plain, err := s.Harness("plain")
	if err != nil {
		t.Fatal(err)
	}
	if plain.BracketedPaste {
		t.Fatal("a runner that said nothing is being treated as asking for bracketed paste")
	}
}

// Existing databases need the migration backfill; DefaultHarnesses only
// seeds new databases.
func TestAnExistingClaudeRowAlreadyDeclaresBracketedPaste(t *testing.T) {
	s := openTestStore(t)

	h, err := s.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	if !h.BracketedPaste {
		t.Fatal("the claude row does not declare bracketed paste, so a pane whose ring " +
			"has wrapped past the enable still pastes raw")
	}

	// THE SHELL ROW IT USED TO CHECK IS GONE, and the backfill it was guarding
	// against still must not invent one. A shell is a property of the machine
	// now, not a runner: see `0053_shell_is_not_a_runner`. A migration that
	// created a row while backfilling a column would be the harder bug, so it
	// is checked here rather than assumed.
	if _, err := s.Harness("shell"); err == nil {
		t.Fatal("something re-created a shell runner. a shell is the machine's, " +
			"held in shell_command and found by shellpick")
	}
}
