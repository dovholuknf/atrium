package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/api"
)

func TestLowLimitNote(t *testing.T) {
	for _, c := range []struct {
		k    int
		want string
	}{{0, ""}, {200, ""}, {150, ""}, {149, "limit 149k from card"}, {100, "limit 100k from card"}} {
		if got := lowLimitNote(c.k, "card"); got != c.want {
			t.Errorf("lowLimitNote(%d) = %q, want %q", c.k, got, c.want)
		}
	}
}

func TestCycleStartEventNamesTheLayers(t *testing.T) {
	ev := cycleStartEvent(118080, 100000, "p", api.ContextLimit{K: 100, From: "card", OwnK: 100, CeilingK: 333})
	if ev["limit_from"] != "card" || ev["own_k"] != 100 || ev["ceiling_k"] != 333 || ev["limit"] != int64(100000) {
		t.Errorf("event = %v", ev)
	}
}
