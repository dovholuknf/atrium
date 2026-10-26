package link

import (
	"math/rand"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/tokenprice"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

var t0 = time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)

func at(min int) time.Time { return t0.Add(time.Duration(min) * time.Minute) }
func stamp(min int) string { return at(min).Format(time.RFC3339Nano) }

// A builder on `building` for the first hour and `tested` for the second.
func builderWindows() []stepWindow {
	return []stepWindow{{step: "building", start: at(0), end: at(60)}, {step: "tested", start: at(60), end: at(120)}}
}

func TestARowInsideOneStepGoesEntirelyToIt(t *testing.T) {
	steps, w := SplitRow(at(10), at(14), builderWindows(), "building")
	if len(steps) != 1 || steps[0] != "building" || w[0] <= 0 {
		t.Fatalf("%v %v", steps, w)
	}
}

func TestARowCrossingABoundaryIsDividedInTheTimeRatio(t *testing.T) {
	// 6 minutes in building, 2 in tested.
	steps, w := SplitRow(at(54), at(62), builderWindows(), "building")
	if len(steps) != 2 || steps[0] != "building" || steps[1] != "tested" || w[0] != 3*w[1] {
		t.Fatalf("%v %v", steps, w)
	}
	parts := tokenprice.Divide(tokenprice.Tokens{Input: 1001, Output: 41, CacheRead: 12345}, w)
	if parts[0].Input != 751 || parts[1].Input != 250 {
		t.Fatalf("input %d / %d", parts[0].Input, parts[1].Input)
	}
}

func TestARowOutsideEveryWindowGoesToTheNearestStep(t *testing.T) {
	if steps, _ := SplitRow(at(-30), at(-20), builderWindows(), "building"); steps[0] != "building" {
		t.Fatalf("before the first: %v", steps)
	}
	if steps, _ := SplitRow(at(200), at(205), builderWindows(), "building"); steps[0] != "tested" {
		t.Fatalf("after the last: %v", steps)
	}
	gap := []stepWindow{{step: "designed", start: at(0), end: at(10)}, {step: "building", start: at(100), end: at(200)}}
	if steps, _ := SplitRow(at(20), at(22), gap, "building"); steps[0] != "designed" {
		t.Fatalf("closer to the first: %v", steps)
	}
	if steps, _ := SplitRow(at(90), at(92), gap, "designed"); steps[0] != "building" {
		t.Fatalf("closer to the second: %v", steps)
	}
	if steps, _ := SplitRow(at(1), at(2), nil, "reviewed"); steps[0] != "reviewed" {
		t.Fatalf("a card with no window is on its home step: %v", steps)
	}
}

func TestAMomentIsInTheStepThatHoldsIt(t *testing.T) {
	if steps, _ := SplitRow(at(70), at(70), builderWindows(), "building"); steps[0] != "tested" {
		t.Fatalf("%v", steps)
	}
}

func cardsAndSteps() ([]hubstore.WorkCard, []hubstore.WorkStepRow) {
	cards := []hubstore.WorkCard{
		{BacklogID: "f-x", Room: "sg4", Card: "b1", Step: "building", AttachedAt: stamp(-5)},
		{BacklogID: "f-x", Room: "sg4", Card: "rv", Step: "reviewed", AttachedAt: stamp(-5)},
		{BacklogID: "f-x", Room: "sg4", Card: "rl", Role: "relay", Step: "building", AttachedAt: stamp(-5)},
	}
	steps := []hubstore.WorkStepRow{
		{Step: "building", State: "done", StartedAt: stamp(0), EndedAt: stamp(60), Cards: `["sg4/b1"]`},
		{Step: "tested", State: "done", StartedAt: stamp(60), EndedAt: stamp(90), Cards: `["sg4/b1"]`},
		{Step: "reviewed", State: "done", StartedAt: stamp(90), EndedAt: stamp(120), Cards: `["sg4/rv"]`},
	}
	return cards, steps
}

// THE ACCEPTANCE: the steps plus coordination equal the sum of the rows' priced tokens, to the cent.
func TestTheStepsPlusCoordinationEqualTheSumOfTheRowsToTheCent(t *testing.T) {
	tb := tokenprice.Table{Rows: tokenprice.Defaults("2026-09-28T00:00:00Z")}
	cards, steps := cardsAndSteps()
	r := rand.New(rand.NewSource(3))
	models := []string{"claude-opus-5-5", "claude-sonnet-5-5[1m]", "claude-fable-5-1", "claude-haiku-4-5-20251001"}
	var rows []hubstore.WorkUsageRow
	var want tokenprice.Money
	for i := 0; i < 400; i++ {
		card, class := []string{"b1", "b1", "b1", "rv", "rl"}[r.Intn(5)], hubstore.ClassStep
		if card == "rl" {
			class = hubstore.ClassCoord
		}
		start := r.Intn(140) - 10
		row := hubstore.WorkUsageRow{Room: "sg4", UsageID: "u", BacklogID: "f-x", Card: card, Class: class,
			Model: models[r.Intn(len(models))], StartedAt: stamp(start), EndedAt: stamp(start + r.Intn(9)),
			Tokens: tokenprice.Tokens{Input: r.Int63n(5000), CacheWrite5m: r.Int63n(90000), CacheWrite1h: r.Int63n(9000),
				CacheRead: r.Int63n(900000), Output: r.Int63n(7000)}}
		p, _ := tb.For(row.Model, row.EndedAt)
		want += p.Cost(row.Tokens)
		rows = append(rows, row)
	}
	v := ItemCost(tb, cards, steps, rows, at(500))
	var units int64
	for _, s := range v.Steps {
		units += s.Units
	}
	if units != v.Headline.Units || units+v.Coordination.Units != v.Total.Units {
		t.Fatalf("steps %d + coordination %d != total %d", units, v.Coordination.Units, v.Total.Units)
	}
	if v.Total.Units != int64(want) || v.Total.Cents != want.Cents() {
		t.Fatalf("total %d (%d cents), the rows are %d (%d cents)", v.Total.Units, v.Total.Cents, int64(want), want.Cents())
	}
	var sum tokenprice.Tokens
	for _, row := range rows {
		sum = sum.Add(row.Tokens)
	}
	if sum != v.Total.Tokens || v.Headline.Tokens.Add(v.Coordination.Tokens) != sum {
		t.Fatalf("a token was lost: rows %+v, steps %+v, coordination %+v", sum, v.Headline.Tokens, v.Coordination.Tokens)
	}
	if !v.Estimated || v.Prices != "prices-2026-09-28" || v.HeadlineSuffix != "+ coordination" {
		t.Fatalf("the figure must say what it is: %+v", v)
	}
}

func TestAModelWithNoPriceShowsAQuestionMarkAndAStepWithNoSpendADash(t *testing.T) {
	tb := tokenprice.Table{Rows: tokenprice.Defaults("2026-09-28T00:00:00Z")}
	cards, steps := cardsAndSteps()
	rows := []hubstore.WorkUsageRow{
		{Room: "sg4", UsageID: "a", BacklogID: "f-x", Card: "b1", Model: "claude-opus-5-5", StartedAt: stamp(5), EndedAt: stamp(6), Tokens: tokenprice.Tokens{Input: 1000}},
		{Room: "sg4", UsageID: "b", BacklogID: "f-x", Card: "rv", Model: "claude-new-9", StartedAt: stamp(100), EndedAt: stamp(101), Tokens: tokenprice.Tokens{Input: 500}},
	}
	v := ItemCost(tb, cards, steps, rows, at(500))
	by := map[string]CostLine{}
	for _, s := range v.Steps {
		by[s.Step] = s.CostLine
	}
	if by["building"].Display != "$0.00" && by["building"].Display[0] != '$' || by["building"].Display == "?" {
		t.Fatalf("building: %q", by["building"].Display)
	}
	if by["reviewed"].Display != "?" || by["reviewed"].TotalTokens != 500 || by["reviewed"].UnpricedModels[0] != "claude-new-9" {
		t.Fatalf("reviewed: %+v", by["reviewed"])
	}
	if by["designed"].Display != "-" || by["landed"].Display != "-" {
		t.Fatalf("no spend must read as a dash: %q %q", by["designed"].Display, by["landed"].Display)
	}
	if v.Headline.Display != "?" || v.Total.Display != "?" {
		t.Fatalf("a sum holding an unpriced model is a question mark: %q %q", v.Headline.Display, v.Total.Display)
	}
	if v.Coordination.Display != "-" {
		t.Fatalf("coordination: %q", v.Coordination.Display)
	}
}
