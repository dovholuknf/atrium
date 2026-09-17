package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// `Say` writes the text and then writes Enter. An Enter landing on a dialog
// answers it with whatever option was highlighted, and nothing reports that it
// happened. These pin the flag that stops it.

func TestADialogBlocksTypingUntilSomethingElseHappens(t *testing.T) {
	tr := newActivityTracker()
	tr.set("t1", ActivityIdle, "")
	if tr.dialogOpen("t1") {
		t.Fatal("a card with no dialog reported one")
	}

	tr.dialogRaised("t1")
	if !tr.dialogOpen("t1") {
		t.Fatal("the dialog was not recorded")
	}

	// THE NEXT THING THE SESSION DOES CLEARS IT. There is no hook for a prompt
	// being dismissed, so this is the only signal there is.
	tr.set("t1", ActivityTool, "Bash")
	if tr.dialogOpen("t1") {
		t.Fatal("a tool call did not clear the dialog, so the card is typed into never again")
	}
}

// THE ORDERING IN `onActivity` IS LOAD BEARING, and this is the test that
// catches it being reversed. `set` clears the flag, so raising the dialog
// before it means raising it and then immediately forgetting.
func TestRaisingADialogAfterSettingActivityIsWhatSticks(t *testing.T) {
	tr := newActivityTracker()

	// The right way round, which is what the waiting branch does.
	tr.set("t1", ActivityIdle, "")
	tr.dialogRaised("t1")
	if !tr.dialogOpen("t1") {
		t.Fatal("raising after set did not stick")
	}

	// The wrong way round, kept here so the failure is legible rather than
	// mysterious if somebody swaps the two lines.
	tr.dialogRaised("t2")
	tr.set("t2", ActivityIdle, "")
	if tr.dialogOpen("t2") {
		t.Fatal("raising before set survived, so this test no longer proves the ordering")
	}
}

// A card atrium has never heard of is not a card with a dialog on it.
func TestAnUnknownCardHasNoDialog(t *testing.T) {
	tr := newActivityTracker()
	if tr.dialogOpen("nobody") {
		t.Fatal("an unknown card reported a dialog")
	}
}

// The flag survives activity it does not describe. `dialogRaised` on a card
// with no activity at all has to create the entry, or the first dialog on a
// freshly seen session is dropped.
func TestADialogOnACardWithNoActivityYetIsStillRecorded(t *testing.T) {
	tr := newActivityTracker()
	tr.dialogRaised("t1")
	if !tr.dialogOpen("t1") {
		t.Fatal("a dialog on a card with no prior activity was dropped")
	}
}

// ATRIUM'S OWN PROMPT IS NOT A DIALOG ON THE SCREEN. While its gate holds a
// request the runner is blocked inside a hook and draws nothing, so a
// notification arriving with a request of ours pending is our own echo.
//
// The store answering an error counts as pending, deliberately: a message
// queued that could have been typed costs a delay, and typing into a dialog
// costs an answer nobody gave.
func TestAPendingRequestOfOurOwnMeansTheDialogIsNotTheRunners(t *testing.T) {
	d := testDaemon(t)
	task, _, err := d.st.Register(store.Observed{
		WireName: "dialog-test", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	if d.hasPendingPermission(task.ID) {
		t.Fatal("a card with no requests reported one pending")
	}

	if _, _, err := d.st.RecordPermission(task.ID, "Bash", "rm -rf /", "", ""); err != nil {
		t.Fatal(err)
	}
	if !d.hasPendingPermission(task.ID) {
		t.Fatal("a recorded request did not read as pending, so atrium would treat its own " +
			"prompt as the runner's and stop typing into a card it could type into")
	}
}
