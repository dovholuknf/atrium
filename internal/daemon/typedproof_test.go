package daemon

import (
	"reflect"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestProvenTypedMatchesOldestSayFirstAndOncePerPrompt(t *testing.T) {
	t.Parallel()
	says := []store.Say{
		{ID: "a", Text: "stop now"},
		{ID: "b", Text: "stop now"},
		{ID: "c", Text: "never read"},
	}
	got := provenTyped(says, []string{"your launcher @x instructs you: stop now", "unrelated"})
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("one prompt must prove one say, oldest first: %v", got)
	}
	if got := provenTyped(says, nil); len(got) != 0 {
		t.Fatalf("no prompts proved %v", got)
	}
}
