package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// WHAT AN ATTACH TELLS THE BOARD ABOUT PASTING, and why it is told rather than
// left to be worked out.
//
// The board wraps a paste in bracketed paste markers so the runner reads it as
// one paste. Its only evidence used to be the `\x1b[?2004h` the runner emits
// once at startup and the pane parsed out of the replayed scrollback, so a pane
// that attached after the ring had wrapped past that byte pasted raw. The
// runner then got the paste in the operating system's own four kilobyte
// installments, which read as a burst of typing per installment: one paste
// arriving as five, and a peer report arriving as its tail.

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

// A SHELL IS NEVER DECLARED FOR IT. `kind=shell` is not the harness's runner,
// it is a command line, and a shell turns the mode on and off around each
// prompt rather than for its whole run. Its enable is also re-emitted at every
// prompt, so the stream is a good answer there and this one would be a guess.
func TestACardsShellIsNeverToldToBracket(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := pasteTask(t, d, "paste-shell", "claude")
	if d.bracketedPasteFor(task.ID, true) {
		t.Fatal("attaching to a card's shell claims the shell asked for bracketed paste. " +
			"the markers would be typed at a prompt that did not request them")
	}
}

// UNKNOWN MEANS NO, which is what keeps `200~` off the screen of a runner that
// never asked. A card whose runner is not a harness atrium knows, and a card id
// that is not a card at all, both fall back to the board believing the stream
// and nothing else.
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
