//go:build integration

package daemon

import (
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// `replies?all=1` is every reply the card's transcripts hold: the whole current one, and the other conversations in
// its folder, oldest first, bounded by the bytes it reads.
func TestRepliesAllHoldsTheWholeTranscriptAndThePriorOnes(t *testing.T) {
	t.Parallel()
	f := newUsageFix(t)
	prior := filepath.Join(filepath.Dir(f.path), "prior.jsonl")
	transcriptReply(t, prior, "p1", f.base.Add(-time.Hour), false, textBlock("from before the clear"))
	for i := 0; i < 30; i++ {
		transcriptReply(t, f.path, fmt.Sprintf("m%d", i), f.base.Add(time.Duration(i)*time.Second), false, textBlock(fmt.Sprintf("reply %d", i)))
	}
	d := &Daemon{hk: *testHooks(), st: f.st, usage: f.u, ctx: newContextSizes()}
	d.initServices()

	v, err := d.repliesAll(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if v.Source != "transcript" || len(v.Replies) != 31 {
		t.Fatalf("want 31 replies (30 current, 1 prior), got %s %d", v.Source, len(v.Replies))
	}
	if v.Replies[0].Text != "from before the clear" || v.Replies[1].Text != "reply 0" || v.Replies[30].Text != "reply 29" {
		t.Fatalf("not oldest first, prior conversation first: %q %q %q", v.Replies[0].Text, v.Replies[1].Text, v.Replies[30].Text)
	}
	// The ordinary page is still the last few.
	if p, _ := d.repliesFor(f.task.ID, 10); len(p.Replies) != 10 {
		t.Fatalf("the ordinary page changed: %d", len(p.Replies))
	}
	// A reply written later is seen: the cache is keyed on the files.
	transcriptReply(t, f.path, "m30", f.base.Add(time.Minute), false, textBlock("reply 30"))
	if v, _ = d.repliesAll(f.task.ID); len(v.Replies) != 32 || v.Replies[31].Text != "reply 30" {
		t.Fatalf("a new reply is missing: %d", len(v.Replies))
	}
}

// The read is bounded: a small byte budget keeps the newest replies and leaves the oldest out, prior files included.
func TestRepliesAllIsBounded(t *testing.T) {
	t.Parallel()
	f := newUsageFix(t)
	prior := filepath.Join(filepath.Dir(f.path), "prior.jsonl")
	transcriptReply(t, prior, "p1", f.base.Add(-time.Hour), false, textBlock("prior"))
	for i := 0; i < 200; i++ {
		transcriptReply(t, f.path, fmt.Sprintf("m%d", i), f.base.Add(time.Duration(i)*time.Second), false,
			textBlock(fmt.Sprintf("reply %d %s", i, strings.Repeat("x", 200))))
	}
	hk := testHooks()
	hk.repliesAllBudget = 4 << 10
	d := &Daemon{hk: *hk, st: f.st, usage: f.u, ctx: newContextSizes()}
	d.initServices()
	v, err := d.repliesAll(f.task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(v.Replies); n == 0 || n >= 200 {
		t.Fatalf("budget not applied: %d replies", n)
	}
	if !strings.HasPrefix(v.Replies[len(v.Replies)-1].Text, "reply 199 ") {
		t.Fatalf("newest reply missing: %q", v.Replies[len(v.Replies)-1].Text)
	}
	for _, r := range v.Replies {
		if r.Text == "prior" {
			t.Fatal("the prior file was read past the budget")
		}
	}
}
