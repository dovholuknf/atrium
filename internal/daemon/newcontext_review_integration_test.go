//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"strings"
	"testing"
)

// After a /clear the card resumes on a new conversation. Its SessionStart must not
// read as proof the clear finished and delete the seeded chip.
func TestASeededChipSurvivesTheNewConversationsSessionStart(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	d.SaveNewContextJournal(map[string]cycle.NcJournalRow{task.ID: {Step: cycle.NewContextWake, File: "HANDOFF.x.md", Conv: "c1"}})

	d.EndAbandonedNewContexts()
	d.WakeSawSession(task.ID, "c2")

	v, _ := d.NewContextFor(task.ID).(map[string]any)
	if v == nil || v["step"] != cycle.NewContextFailed {
		t.Fatalf("the seeded chip was deleted by the new conversation's SessionStart: %v", d.NewContextFor(task.ID))
	}
}

// The idle parking's capture is never journalled, not even before it is marked,
// and a person can still type while it waits.
func TestACaptureOnlyRunIsNeverJournalledAndNeverHoldsTyping(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)

	if _, ok := d.Nctx.BeginCaptureOnly(task.ID, "HANDOFF.x.md", "c1"); !ok {
		t.Fatal("could not begin")
	}
	if j := journalOf(t, d); strings.Contains(j, task.ID) {
		t.Fatalf("a capture-only run was journalled: %q", j)
	}
	if d.Nctx.TypingHeld(task.ID) {
		t.Fatal("typing is held during the idle parking's capture")
	}
	if !d.Nctx.Holding(task.ID) {
		t.Fatal("the run is no longer holding messages")
	}
}
