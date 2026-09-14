package store

import "testing"

// WHICH RUNNERS ASK FOR BRACKETED PASTE, which is a property of the program and
// not something the board can read off the wire.
//
// The board's other source is the `\x1b[?2004h` the runner emits once at
// startup, parsed out of the replayed scrollback. Once the ring has wrapped
// past that byte a freshly opened pane has no evidence at all and pastes raw,
// and a long paste then reaches the runner in four kilobyte installments that
// read as separate bursts of typing. So the answer has to be a row.

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

// THE BACKFILL IS WHAT MAKES THIS WORK ON A DATABASE THAT ALREADY EXISTS.
// `DefaultHarnesses` is seeded once, on first run, so without the migration's
// UPDATE the operator's own `claude` row gains the column and nothing that uses
// it, and the runner this defect was reported against keeps pasting raw.
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

	// A shell is deliberately not in the backfill. It turns the mode on and off
	// around each prompt rather than for its whole run, so it is not a property
	// of the program, and its enable is re-emitted often enough to stay in the
	// ring.
	sh, err := s.Harness("shell")
	if err != nil {
		t.Fatal(err)
	}
	if sh.BracketedPaste {
		t.Fatal("the shell row was backfilled, and a shell's paste mode is not a fact " +
			"about the program")
	}
}
