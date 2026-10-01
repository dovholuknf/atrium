package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// transcriptQueued appends the line Claude Code writes for a message typed while a turn was running.
func transcriptQueued(t *testing.T, path string, at time.Time, prompt string) {
	t.Helper()
	b, _ := json.Marshal(map[string]any{"type": "attachment", "timestamp": at.Format(time.RFC3339Nano),
		"attachment": map[string]any{"type": "queued_command", "commandMode": "prompt", "prompt": prompt}})
	fh, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

// A message typed mid-turn is a queued_command attachment, not a user line. It still starts a turn, so the
// reply to it does not carry the edits of the turn before, and it is one of the prompts /replies lists.
func TestAReplyToAQueuedMessageEditedNothing(t *testing.T) {
	f := newChangesFix(t)
	a := filepath.Join(f.repo, "a.txt")
	writeF(t, a, "changed\n")
	f.turn(changesBase, "t1", toolUse("Edit", a))
	at := changesBase.Add(10 * time.Second)
	transcriptQueued(t, f.tr, at, "review comment on a.txt line 1:\n    +changed")
	transcriptReply(t, f.tr, "t2-text", at.Add(time.Second), false, textBlock("ok"))
	rv, err := f.d.repliesFor(f.task.ID, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(rv.Replies) != 2 || rv.Replies[0].Edited != 1 || rv.Replies[1].Edited != 0 {
		t.Fatalf("edited per reply: %+v", rv.Replies)
	}
	found := false
	for _, p := range rv.Prompts {
		found = found || (strings.HasPrefix(p.Text, "review comment on a.txt") && p.Kind == PromptOperator && p.At.Equal(at.UTC()))
	}
	if !found {
		t.Fatalf("the queued message is not among the prompts: %+v", rv.Prompts)
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
