package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// An attach must report harness paste support even when the startup enable
// sequence has already fallen out of scrollback.

func pasteTask(t *testing.T, d *Daemon, name, runner string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: "/tmp/atrium-test", Runner: runner,
	})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// The case the defect was reported against: a claude session, whose harness row
// declares the mode.
func TestAnAttachSaysWhenTheRunnerAsksForBracketedPaste(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := pasteTask(t, d, "paste-claude", "claude")
	if !d.bracketedPasteFor(task.ID, false) {
		t.Fatal("an attach to a claude session does not say the runner wants bracketed " +
			"paste, so a pane whose scrollback no longer holds the enable pastes raw " +
			"and a long paste is delivered a line at a time")
	}
}

// Shell panes rely on stream detection because paste mode changes around prompts.
func TestACardsShellIsNeverToldToBracket(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := pasteTask(t, d, "paste-shell", "claude")
	if d.bracketedPasteFor(task.ID, true) {
		t.Fatal("attaching to a card's shell claims the shell asked for bracketed paste. " +
			"the markers would be typed at a prompt that did not request them")
	}
}

// Unknown runners and missing cards return false, leaving paste detection
// to the stream instead of sending potentially unsupported markers.
func TestAnUndeclaredRunnerIsNotBracketed(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := pasteTask(t, d, "paste-unknown", "something-else")
	if d.bracketedPasteFor(task.ID, false) {
		t.Fatal("a runner with no harness row is being declared as wanting bracketed paste")
	}
	if d.bracketedPasteFor("no-such-card", false) {
		t.Fatal("a card that does not exist is being declared as wanting bracketed paste")
	}
}
