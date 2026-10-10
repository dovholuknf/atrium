//go:build integration

package daemon

import "testing"

// The attach tells the board how long to hold the cursor, from the runner's
// profile. Codex shows its cursor at cells it is only drawing, claude never
// does, and a card's shell is neither.
func TestAnAttachCarriesTheRunnersCursorSettle(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	codex := pasteTask(t, d, "settle-codex", "codex")
	if got := d.cursorSettleFor(codex.ID, false); got <= 0 {
		t.Fatalf("a codex attach holds the cursor for %dms, so its cursor jumps across the input box", got)
	}
	if got := d.cursorSettleFor(codex.ID, true); got != 0 {
		t.Fatalf("a codex card's shell holds the cursor for %dms, and a shell is not codex", got)
	}
	claude := pasteTask(t, d, "settle-claude", "claude")
	if got := d.cursorSettleFor(claude.ID, false); got != 0 {
		t.Fatalf("a claude attach holds the cursor for %dms, which claude has never needed", got)
	}
	other := pasteTask(t, d, "settle-other", "something-else")
	if got := d.cursorSettleFor(other.ID, false); got != 0 {
		t.Fatalf("a runner with no profile holds the cursor for %dms", got)
	}
	if got := d.cursorSettleFor("no-such-card", false); got != 0 {
		t.Fatalf("a card that does not exist holds the cursor for %dms", got)
	}
}
