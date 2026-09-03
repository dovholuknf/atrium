package store

import (
	"strings"
	"testing"
)

// Tags are free text typed by a person, twice, months apart. "Lab" and "lab"
// showing up as two groups is the failure this feature would be judged on, so
// everything is folded to one form on the way in.
func TestNormalizeTags(t *testing.T) {
	cases := []struct {
		in   []string
		want string
	}{
		{[]string{"Lab", "lab", "LAB"}, "lab"},
		{[]string{"  discourse  "}, "discourse"},
		{[]string{"", "   ", "prs"}, "prs"},
		// Sorted, so two cards with the same tags in a different order group
		// and compare the same.
		{[]string{"zendesk", "lab"}, "lab,zendesk"},
		// Commas separate tags in every text box, so one inside a tag would
		// come back as two after a round trip.
		{[]string{"support,case"}, "support case"},
		// Runs of whitespace collapse, or "pull  request" and "pull request"
		// are two groups.
		{[]string{"pull   request"}, "pull request"},
		{nil, ""},
	}
	for _, c := range cases {
		got := strings.Join(NormalizeTags(c.in), ",")
		if got != c.want {
			t.Errorf("NormalizeTags(%v) = %q, wanted %q", c.in, got, c.want)
		}
	}
}

// Never nil, so the board can filter without a guard and the JSON carries an
// empty list rather than null.
func TestNormalizeTagsIsNeverNil(t *testing.T) {
	if got := NormalizeTags(nil); got == nil {
		t.Fatal("NormalizeTags(nil) returned nil rather than an empty list")
	}
}

// A tag is a label, not a paragraph. Bounded so one pasted mistake cannot
// stretch every row that carries it.
func TestNormalizeTagsAreBounded(t *testing.T) {
	got := NormalizeTags([]string{strings.Repeat("a", 200)})
	if len(got) != 1 || len(got[0]) > 40 {
		t.Fatalf("a 200 character tag came back as %d characters", len(got[0]))
	}
}

// Tags survive a restart like everything else the daemon keeps, and they
// replace rather than merge so removing one is possible.
func TestTagsRoundTripAndReplace(t *testing.T) {
	s := open(t)

	task, _, err := s.Register(Observed{WireName: "tagged", Worktree: "/tmp/x", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if len(task.Tags) != 0 {
		t.Fatalf("a new card came with tags: %v", task.Tags)
	}

	if err := s.SetTags(task.ID, []string{"Lab", "prs"}); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Tags, ",") != "lab,prs" {
		t.Fatalf("tags came back as %v", got.Tags)
	}

	// Replacing with a shorter set has to remove, not merge.
	if err := s.SetTags(task.ID, []string{"prs"}); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(got.Tags, ",") != "prs" {
		t.Fatalf("replacing tags merged instead: %v", got.Tags)
	}
}

// Tagging is the operator filing a card, not the session doing anything.
// Bumping activity would move a silent card to the top of a list sorted by it.
func TestTaggingDoesNotCountAsActivity(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "quiet", Worktree: "/tmp/x", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	before := task.LastActivityAt

	if err := s.SetTags(task.ID, []string{"lab"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetPinned(task.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.LastActivityAt.Equal(before) {
		t.Fatal("filing a card counted as the session doing something")
	}
}

// Pinning is a fixture marker and has to outlive a restart.
func TestPinnedRoundTrips(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "fixture", Worktree: "/tmp/x", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Pinned {
		t.Fatal("a new card came pinned")
	}
	if err := s.SetPinned(task.ID, true); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Pinned {
		t.Fatal("pinning did not stick")
	}
}
