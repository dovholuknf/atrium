package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Restarting a card atrium owns no terminal for has to say so, not fail deeper
// with a message about a directory or a resume id. A window-mode launch owns
// itself and a joined session belongs to whoever started it, so there is no
// terminal here to exit and nothing to relaunch.
func TestRestartRunnerRefusesACardItDoesNotOwn(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "unowned", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	_, err = d.RestartRunner(task.ID)
	if err == nil {
		t.Fatal("restarting a card with no runner succeeded")
	}
	if !strings.Contains(err.Error(), "nothing") {
		t.Fatalf("the refusal does not explain there is nothing to restart: %v", err)
	}
}

// A card that does not exist names the card rather than failing somewhere deeper.
func TestRestartRunnerOnAMissingCardSaysSo(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if _, err := d.RestartRunner("no-such-card"); err == nil {
		t.Fatal("restarting a card that does not exist succeeded")
	}
}
