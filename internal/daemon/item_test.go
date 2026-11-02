package daemon

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// An item worker is never told it has a launcher, in the ending or anywhere else atrium words its finish.
func TestItemEndingLineNeverNamesTheLauncher(t *testing.T) {
	if strings.Contains(strings.ToLower(ItemEndingLine), "launcher") {
		t.Fatalf("the item ending mentions the launcher:\n%s", ItemEndingLine)
	}
	for _, want := range []string{"atrium_done", "atrium_blocked", "Then stop."} {
		if !strings.Contains(ItemEndingLine, want) {
			t.Errorf("the item ending lost %q", want)
		}
	}
	got := BriefPrompt("BRIEF.md", "do it")
	if got != briefPromptEnding("BRIEF.md", "do it", LaunchEndingLine) {
		t.Errorf("an ordinary brief prompt changed")
	}
	item := ItemBriefPrompt("BRIEF.md", "do it")
	if strings.Contains(strings.ToLower(item), "launcher") || !strings.HasSuffix(item, ItemEndingLine) {
		t.Errorf("an item brief prompt is %q", item)
	}
	// The ending is never doubled, and never doubled with the ordinary one.
	if again := ItemBriefPrompt("BRIEF.md", "do it\n\n"+ItemEndingLine); strings.Count(again, ItemEndingLine) != 1 {
		t.Errorf("the item ending was doubled: %q", again)
	}
}

// The opening prompt's sender is the launcher for an ordinary launch and atrium for an item.
func TestOpeningSender(t *testing.T) {
	for _, c := range []struct {
		name string
		req  shared.LaunchRequest
		want string
	}{
		{"ordinary", shared.LaunchRequest{SpawnedBy: "boss"}, "boss"},
		{"operator", shared.LaunchRequest{}, ""},
		{"human", shared.LaunchRequest{SpawnedBy: store.HumanLauncher}, ""},
		{"item flag", shared.LaunchRequest{SpawnedBy: "boss", Item: true}, store.ReservedHandle},
		{"item tag", shared.LaunchRequest{SpawnedBy: "boss", Tags: []string{ItemTag}}, store.ReservedHandle},
	} {
		if got := openingSender(c.req); got != c.want {
			t.Errorf("%s: sender %q, want %q", c.name, got, c.want)
		}
	}
}

func TestIsItemReadsTheTag(t *testing.T) {
	if !isItem(&store.Task{Tags: []string{"x", ItemTag}}) || isItem(&store.Task{Tags: []string{"x"}}) || isItem(nil) {
		t.Fatal("isItem does not follow the tag")
	}
}
