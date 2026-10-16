package link

import (
	"strings"
	"testing"
	"time"
)

// A destructive verb refuses a name two cards answer to, lists each with its id,
// room, status and age, and still takes an id or a wire name.
func TestAmbiguousAliasRefusesTwoCardsAndListsThem(t *testing.T) {
	now := time.Date(2026, 10, 16, 12, 0, 0, 0, time.UTC)
	cards := []ctlCard{
		{ID: "m1~old", Wire: "x/minion-a", Alias: "minion", Status: "running", Created: "2026-10-14T12:00:00Z"},
		{ID: "m1~new", Wire: "x/minion-b", Alias: "minion", Status: "needs-input", Created: "2026-10-16T09:00:00Z"},
		{ID: "m1~gone", Wire: "x/minion-c", Alias: "minion", Status: "dead", Created: "2026-10-01T09:00:00Z"},
		{ID: "m1~solo", Wire: "x/solo", Alias: "solo", Status: "running", Created: "2026-10-16T09:00:00Z"},
	}
	err := ambiguousAlias(cards, "@Minion", now)
	if err == nil {
		t.Fatal("two live cards called minion were not refused")
	}
	for _, want := range []string{"m1~old", "m1~new", "room m1", "running", "needs-input", "2d old", "3h old"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("refusal %q does not say %q", err, want)
		}
	}
	if strings.Contains(err.Error(), "m1~gone") {
		t.Errorf("a dead card was offered: %v", err)
	}
	for _, who := range []string{"solo", "m1~old", "x/minion-b", "nobody"} {
		if err := ambiguousAlias(cards, who, now); err != nil {
			t.Errorf("%q should not be ambiguous: %v", who, err)
		}
	}
}
