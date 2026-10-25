package tokenprice

import (
	"math/rand"
	"testing"
)

func TestDivideLosesNoTokenAndTheRemainderGoesToTheLargerPart(t *testing.T) {
	in := Tokens{Input: 1001, CacheWrite5m: 7, CacheWrite1h: 1, CacheRead: 999983, Output: 3}
	got := Divide(in, []int64{1, 2})
	var sum Tokens
	for _, g := range got {
		sum = sum.Add(g)
	}
	if sum != in {
		t.Fatalf("parts %+v sum to %+v, not %+v", got, sum, in)
	}
	// 1001 * 1/3 is 333 with 2 left over, and the larger part (weight 2) takes the 2.
	if got[0].Input != 333 || got[1].Input != 668 {
		t.Fatalf("input split = %d and %d", got[0].Input, got[1].Input)
	}
	if got[0].Output != 1 || got[1].Output != 2 {
		t.Fatalf("output split = %d and %d", got[0].Output, got[1].Output)
	}
}

func TestDivideIsExactForAnyWeightsAndNeverOverflows(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	for i := 0; i < 500; i++ {
		in := Tokens{Input: r.Int63n(1e10), CacheWrite5m: r.Int63n(1e10), CacheWrite1h: r.Int63n(1e10), CacheRead: r.Int63n(1e11), Output: r.Int63n(1e9)}
		n := 1 + r.Intn(5)
		w := make([]int64, n)
		for j := range w {
			w[j] = r.Int63n(5 * 3600 * 1000)
		}
		var sum Tokens
		for _, g := range Divide(in, w) {
			sum = sum.Add(g)
		}
		if sum != in {
			t.Fatalf("weights %v: %+v != %+v", w, sum, in)
		}
	}
	if got := Divide(Tokens{Input: 5}, []int64{0, 0}); got[0].Input != 5 {
		t.Fatalf("all-zero weights lost the tokens: %+v", got)
	}
}

func TestAModelIsMatchedByItsNameAndADateStampNotByAPrefix(t *testing.T) {
	tb := Table{Rows: Defaults("2026-09-28T00:00:00Z")}
	for model, want := range map[string]string{
		"claude-sonnet-5":           "claude-sonnet-5",
		"claude-sonnet-5-5":         "claude-sonnet-5-5",
		"claude-sonnet-5-5[1m]":     "claude-sonnet-5-5",
		"claude-haiku-4-5-20251001": "claude-haiku-4-5",
		"Claude-Opus-5-5":           "claude-opus-5-5",
		"claude-fable-5-1":          "claude-fable-5-1",
	} {
		p, ok := tb.For(model, "")
		if !ok || p.Model != want {
			t.Errorf("%s matched %q (%v), want %s", model, p.Model, ok, want)
		}
	}
	for _, model := range []string{"", "claude-sonnet-5-6", "claude-opus-4", "gpt-5", "claude-fable-5-10"} {
		if _, ok := tb.For(model, ""); ok {
			t.Errorf("%q has no price and must not be given one", model)
		}
	}
}

func TestAnUnpricedModelShowsAQuestionMarkAndNoSpendShowsADash(t *testing.T) {
	tb := Table{Rows: Defaults("2026-09-28T00:00:00Z")}
	var p Priced
	if p.Display() != "-" {
		t.Fatalf("nothing spent shows %q", p.Display())
	}
	p.Add(tb, "claude-sonnet-5-5", "", Tokens{Input: 1_000_000})
	if p.Display() != "$2.00" {
		t.Fatalf("a million sonnet input tokens is %q", p.Display())
	}
	p.Add(tb, "some-new-model", "", Tokens{Input: 10})
	if p.Display() != "?" || p.Known() || p.Unpriced != 10 || len(p.Models) != 1 || p.Models[0] != "some-new-model" {
		t.Fatalf("an unpriced model: %q %+v", p.Display(), p)
	}
	if p.Tokens.Input != 1_000_010 {
		t.Fatalf("the tokens are kept beside the question mark: %+v", p.Tokens)
	}
}

func TestEveryTokenTypeIsPricedAtItsOwnRate(t *testing.T) {
	p := Price{Input: 4, CacheWrite5m: 5, CacheWrite1h: 8, CacheRead: 0.2, Output: 20}
	got := p.Cost(Tokens{Input: 1e6, CacheWrite5m: 1e6, CacheWrite1h: 1e6, CacheRead: 1e6, Output: 1e6})
	if got.Cents() != 3720 { // 4 + 5 + 8 + 0.2 + 20 = 37.20
		t.Fatalf("cost = %v", got.Dollars())
	}
}

func TestThePriceInForceIsTheNewestAtOrBeforeTheTurn(t *testing.T) {
	tb := Table{Rows: []Price{
		{Model: "m", ValidFrom: "2026-01-01T00:00:00Z", Input: 1},
		{Model: "m", ValidFrom: "2026-06-01T00:00:00Z", Input: 2},
	}}
	if p, _ := tb.For("m", "2026-03-01T00:00:00Z"); p.Input != 1 {
		t.Fatalf("march: %v", p.Input)
	}
	if p, _ := tb.For("m", "2026-07-01T00:00:00Z"); p.Input != 2 {
		t.Fatalf("july: %v", p.Input)
	}
	if p, _ := tb.For("m", "2025-01-01T00:00:00Z"); p.Input != 1 {
		t.Fatalf("before any row: %v", p.Input)
	}
	if tb.Version() != "prices-2026-06-01" {
		t.Fatalf("version = %s", tb.Version())
	}
}
