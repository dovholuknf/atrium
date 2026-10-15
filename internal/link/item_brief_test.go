package link

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

func testedAndReviewed() WorkDetail {
	d := WorkDetail{Work: hubstore.Work{BacklogID: "f-x", Title: "a thing", Branch: "claude/x"}}
	d.Life.State, d.Life.Current = "running", "landed"
	d.Steps = []hubstore.WorkStepRow{
		{Step: "building", State: "done", SHA: "1234567890abcdef"},
		{Step: "tested", State: "done", SHA: "1234567890abcdef"},
		{Step: "reviewed", State: "done", SHA: "1234567890abcdef"},
		{Step: "landed", State: "active", Party: "card"},
	}
	d.Facts = []hubstore.WorkFact{
		{Kind: "test", Source: "seen", AtSHA: "1234567890abcdef"},
		{Kind: "review", Source: "reported", AtSHA: "1234567890abcdef", Dirty: true},
	}
	return d
}

func TestItemBriefAnswersWhereAreWe(t *testing.T) {
	got := RenderItemBrief(testedAndReviewed())
	for _, want := range []string{"Item `f-x`: a thing (running, at landed)", "Branch: `claude/x`",
		"tested: done at 1234567. test recorded (seen) at 1234567",
		"reviewed: done at 1234567. review recorded (reported) at 1234567 with uncommitted changes",
		"designed: pending", "landed: active, waiting on card", "pushed: pending",
		"~/.atrium/items/f-x/", "atrium_item show f-x"} {
		if !strings.Contains(got, want) {
			t.Errorf("brief lacks %q:\n%s", want, got)
		}
	}
	if n := strings.Count(got, "\n- "); n != 8 {
		t.Errorf("want eight step lines, got %d", n)
	}
}

func TestItemBriefOnceAndNeverFailsALaunch(t *testing.T) {
	h := RenderItemBrief(testedAndReviewed())
	b := WithItemBrief("do it", h)
	if !strings.HasPrefix(b, itemBriefMark) || WithItemBrief(b, h) != b {
		t.Errorf("header goes first, once: %q", b)
	}
	down := func(context.Context, string, string, any, any) error { return errors.New("hub down") }
	if got := ItemBrief(context.Background(), down, "f-x"); got != "" {
		t.Errorf("a hub that cannot answer gives no header, got %q", got)
	}
	if WithItemBrief("do it", "") != "do it" {
		t.Error("an empty header leaves the brief alone")
	}
}

func TestItemBriefGivesUpOnAHubThatNeverAnswers(t *testing.T) {
	if itemBriefWait != 5*time.Second {
		t.Fatalf("item brief wait is %v, want 5s", itemBriefWait)
	}
	silent := func(ctx context.Context, method, path string, body, out any) error {
		<-ctx.Done()
		return ctx.Err()
	}
	start := time.Now()
	got := itemBriefWithin(context.Background(), silent, "f-x", 3*time.Millisecond)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("launch waited %v for a hub that never answers", el)
	}
	if !strings.Contains(got, "did not answer in time") || strings.Count(strings.TrimSpace(got), "\n") != 0 {
		t.Fatalf("want a one line note, got %q", got)
	}
	if b := WithItemBrief("do it", got); !strings.HasSuffix(b, "do it") || !strings.Contains(b, "unavailable") {
		t.Fatalf("brief %q", b)
	}
}
