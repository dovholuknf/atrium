package daemon

import (
	"strings"
	"testing"
)

// The refusal says what happened and what it means, and leaves what to press to
// the buttons.
//
// The wording is pinned because the old one was the complaint: "braid its
// transcript into a thread neither of them wrote" is three figures of speech in
// nine words, and a reader who does not already know what a transcript is gets
// nothing from any of them.
func TestTheResumeRefusalHasNoMetaphorInIt(t *testing.T) {
	err := &ResumeBusy{
		HolderID:    "t1",
		HolderTitle: "discourse-6080",
		Worktree:    "D:/worktrees/github/openziti/ziti/discourse-6080",
		Resume:      "abc-123",
	}
	msg := err.Error()

	for _, banned := range []string{"braid", "thread", "weave", "shred"} {
		if strings.Contains(strings.ToLower(msg), banned) {
			t.Errorf("the refusal still says %q: %s", banned, msg)
		}
	}
	// It names the card, and the directory, because the title is a display
	// title and two cards can plausibly wear it.
	if !strings.Contains(msg, "discourse-6080") {
		t.Errorf("the refusal does not say which card holds it: %s", msg)
	}
	if !strings.Contains(msg, "discourse-6080") || !strings.Contains(msg, "worktrees") {
		t.Errorf("the refusal does not say where that card is: %s", msg)
	}
	// It states the mechanism rather than painting it.
	if !strings.Contains(msg, "interleave") {
		t.Errorf("the refusal does not say what actually goes wrong: %s", msg)
	}
}

// A card with no directory recorded still produces a readable sentence rather
// than an empty bracket.
func TestTheResumeRefusalReadsWithNoWorktree(t *testing.T) {
	err := &ResumeBusy{HolderTitle: "discourse-6080", Resume: "abc-123"}
	msg := err.Error()
	if strings.Contains(msg, "()") {
		t.Errorf("an empty directory left an empty bracket: %s", msg)
	}
	if !strings.Contains(msg, "discourse-6080") {
		t.Errorf("the refusal lost the card name: %s", msg)
	}
}

// THE FIELDS ARE THE POINT. The board draws two buttons off these, so a
// refusal that carries no holder id is a dialog that can only say `ok`.
func TestTheResumeRefusalCarriesWhatTheButtonsNeed(t *testing.T) {
	err := &ResumeBusy{
		HolderID: "t1", HolderTitle: "discourse-6080",
		Worktree: "D:/wt", Resume: "abc-123",
	}
	got := err.ResumeConflict()

	if got["kind"] != "resume-busy" {
		t.Errorf("kind is %v, and the board keys on it", got["kind"])
	}
	// `attach to that one` is `attachTask(holder_id)`, which needs the id and
	// not the title.
	if got["holder_id"] != "t1" {
		t.Errorf("holder_id is %v, so attach cannot go anywhere", got["holder_id"])
	}
	// The conversation id is the one thing the operator cannot see anywhere
	// else on screen.
	if got["resume"] != "abc-123" {
		t.Errorf("resume is %v, so the dialog cannot name the conversation", got["resume"])
	}
}

// The API recognises this by METHOD, not by type, so the daemon can grow a
// second actionable refusal without the API learning its name. If this stops
// compiling, the API's type assertion has quietly stopped matching.
func TestTheRefusalSatisfiesTheInterfaceTheAPIChecksFor(t *testing.T) {
	var err error = &ResumeBusy{HolderID: "t1"}
	if _, ok := err.(interface{ ResumeConflict() map[string]any }); !ok {
		t.Fatal("the API tests for this method and would send a bare 400 instead")
	}
}
