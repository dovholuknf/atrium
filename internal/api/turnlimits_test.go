package api

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func limitRows() []store.CostRow {
	return []store.CostRow{
		{Day: "2026-10-10", Kind: "say", Turns: 60, Input: 100, Output: 900, CacheWrite: 1000, CacheRead: 9_000_000},
		{Day: "2026-10-10", Kind: "say", TaskID: "b", Turns: 50, Input: 1, Output: 1},
		{Day: "2026-10-10", Kind: "nudge", Turns: 3, Output: 9_000_000},
		{Day: "2026-10-10", Kind: store.DeliveryOperator, Turns: 9999, Output: 99_999_999},
		{Day: "2026-10-09", Kind: "report", Turns: 9999},
	}
}

func TestTurnLimitFiresPastTheTurnLimit(t *testing.T) {
	lim, err := store.ParseTurnLimits(`{"turns":100,"tokens":0}`)
	if err != nil {
		t.Fatal(err)
	}
	got := CheckTurnLimits(limitRows(), "2026-10-10", lim)
	if len(got) != 1 || got[0].Kind != "say" || got[0].Turns != 110 || got[0].Over != "turns" || got[0].LimitTurns != 100 {
		t.Fatalf("findings %+v", got)
	}
	// Tokens leave out cache reads, as the usage tab counts them.
	if got[0].Tokens != 100+900+1000+1+1 {
		t.Fatalf("tokens %d", got[0].Tokens)
	}
}

func TestTurnLimitFiresPastTheTokenLimitAndNamesTheItem(t *testing.T) {
	lim, _ := store.ParseTurnLimits(`{"turns":0,"tokens":5000000}`)
	rows := []store.CostRow{{Day: "d", Kind: "notice", Turns: 2, Output: 6_000_000}}
	got := CheckTurnLimits(rows, "d", lim)
	if len(got) != 1 || got[0].Over != "tokens" || got[0].Item != "" {
		t.Fatalf("findings %+v", got)
	}
	// A kind with an open item names it.
	items := map[string]string{"notice": "r-some-open-item"}
	if got := checkTurnLimits(rows, "d", lim, items); len(got) != 1 || got[0].Item != "r-some-open-item" {
		t.Fatalf("a kind with an item: %+v", got)
	}
}

func TestTurnLimitDoesNotFireUnderTheLimitOrOnAnotherDayOrForTheOperator(t *testing.T) {
	lim, _ := store.ParseTurnLimits("")
	if lim.Turns != store.DefaultTurnLimit || lim.Tokens != store.DefaultTokenLimit {
		t.Fatalf("default %+v", lim)
	}
	// 110 say turns is over 100, but the nudge's 9M tokens is the only one over 5M: both fire by default.
	if got := CheckTurnLimits(limitRows(), "2026-10-10", lim); len(got) != 2 {
		t.Fatalf("default findings %+v", got)
	}
	big, _ := store.ParseTurnLimits(`{"turns":1000,"tokens":50000000}`)
	if got := CheckTurnLimits(limitRows(), "2026-10-10", big); len(got) != 0 {
		t.Fatalf("under the limit fired %+v", got)
	}
}

func TestTurnLimitOffNeverFires(t *testing.T) {
	lim, err := store.ParseTurnLimits("off")
	if err != nil {
		t.Fatal(err)
	}
	if got := CheckTurnLimits(limitRows(), "2026-10-10", lim); len(got) != 0 {
		t.Fatalf("off fired %+v", got)
	}
}

func TestTurnLimitPerKindOverride(t *testing.T) {
	lim, err := store.ParseTurnLimits(`{"turns":1000,"tokens":0,"kinds":{"nudge":{"turns":2}}}`)
	if err != nil {
		t.Fatal(err)
	}
	got := CheckTurnLimits(limitRows(), "2026-10-10", lim)
	if len(got) != 1 || got[0].Kind != "nudge" || got[0].LimitTurns != 2 {
		t.Fatalf("findings %+v", got)
	}
}

func TestTurnLimitRefusesWhatItCannotRead(t *testing.T) {
	for _, v := range []string{"lots", `{"turns":-1}`, `{"kinds":{"say":{"tokens":-5}}}`} {
		if _, err := store.ParseTurnLimits(v); err == nil {
			t.Fatalf("%q was taken", v)
		}
	}
}
