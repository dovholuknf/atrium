package daemon

import (
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A message that reaches the card with an origin other than human (a quote sent from the phone) still starts a
// turn, so the reply to it does not carry the edits of the turn before.
func TestAReplyToAMessageWithAnotherOriginEditedNothing(t *testing.T) {
	f := newChangesFix(t)
	a := filepath.Join(f.repo, "a.txt")
	writeF(t, a, "changed\n")
	f.turn(changesBase, "t1", toolUse("Edit", a))
	at := changesBase.Add(10 * time.Second)
	transcriptUser(t, f.tr, at, "review comment on a.txt line 1:\n    +changed", map[string]any{"origin": map[string]any{"kind": "channel"}})
	transcriptReply(t, f.tr, "t2-text", at.Add(time.Second), false, textBlock("ok"))
	rv, err := f.d.repliesFor(f.task.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rv.Replies) != 2 || rv.Replies[0].Edited != 1 || rv.Replies[1].Edited != 0 {
		t.Fatalf("edited per reply: %+v", rv.Replies)
	}
}

// The cut note's reason carries no counts, since the sheet words those itself.
func TestCutWhyHoldsNoCounts(t *testing.T) {
	files := make([]ChangeFile, changesFilesMax+5)
	for i := range files {
		files[i].Hunks = "x"
	}
	_, cut := bound(files)
	if cut == nil || cut.Files != 5 || strings.Contains(cut.Why, "5 files") || strings.Contains(cut.Why, "counts only") {
		t.Fatalf("%+v", cut)
	}
}
