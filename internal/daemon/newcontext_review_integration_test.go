//go:build integration

package daemon

import (
	"strings"
	"testing"
)

// After a /clear the card resumes on a new conversation. Its SessionStart must not
// read as proof the clear finished and delete the seeded chip.
func TestASeededChipSurvivesTheNewConversationsSessionStart(t *testing.T) {
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)
	d.saveNewContextJournal(map[string]ncJournalRow{task.ID: {Step: NewContextWake, File: "HANDOFF.x.md", Conv: "c1"}})

	d.endAbandonedNewContexts()
	d.wakeSawSession(task.ID, "c2")

	v, _ := d.newContextFor(task.ID).(map[string]any)
	if v == nil || v["step"] != NewContextFailed {
		t.Fatalf("the seeded chip was deleted by the new conversation's SessionStart: %v", d.newContextFor(task.ID))
	}
}

// The idle parking's capture is never journalled, not even before it is marked,
// and a person can still type while it waits.
func TestACaptureOnlyRunIsNeverJournalledAndNeverHoldsTyping(t *testing.T) {
	d := testDaemon(t)
	task, _, _ := ncCard(t, d)

	if _, ok := d.nctx.beginCaptureOnly(task.ID, "HANDOFF.x.md", "c1"); !ok {
		t.Fatal("could not begin")
	}
	if j := journalOf(t, d); strings.Contains(j, task.ID) {
		t.Fatalf("a capture-only run was journalled: %q", j)
	}
	if d.nctx.typingHeld(task.ID) {
		t.Fatal("typing is held during the idle parking's capture")
	}
	if !d.nctx.holding(task.ID) {
		t.Fatal("the run is no longer holding messages")
	}
}
