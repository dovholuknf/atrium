//go:build integration

package hubstore

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/base/tokenprice"
)

// THE PRICE TABLE COMES WITH ROWS, OPUS AND FABLE AMONG THEM, and nothing ships priced as zero.
func TestThePriceTableIsSeededWithEveryModelPriced(t *testing.T) {
	s := open(t)
	tb, err := s.Prices()
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []string{"claude-haiku-4-5", "claude-sonnet-5", "claude-sonnet-5-5", "claude-opus-5-5", "claude-fable-5-1"} {
		p, ok := tb.For(m, "")
		if !ok || p.Input <= 0 || p.CacheWrite5m <= p.Input || p.CacheRead <= 0 || p.Output <= p.Input {
			t.Fatalf("%s: %+v %v", m, p, ok)
		}
	}
	if tb.Version() != "prices-2026-09-28" {
		t.Fatal(tb.Version())
	}
	// A repricing is a new row, and the rows before it are priced as they were.
	if err := s.PutPrice(tokenprice.Price{Model: "claude-opus-5-5", ValidFrom: "2026-11-01T00:00:00Z", Input: 3, CacheWrite5m: 3.75, CacheWrite1h: 6, CacheRead: 0.3, Output: 15}); err != nil {
		t.Fatal(err)
	}
	tb, _ = s.Prices()
	if p, _ := tb.For("claude-opus-5-5", "2026-10-01T00:00:00Z"); p.Input != 4 {
		t.Fatalf("an old turn was repriced: %v", p.Input)
	}
	if p, _ := tb.For("claude-opus-5-5", "2026-11-02T00:00:00Z"); p.Input != 3 {
		t.Fatalf("a new turn: %v", p.Input)
	}
}

// A replay of rows adds nothing, and a delivery whose counts grew is updated once.
func TestWorkUsageIsKeptOnceAndADeliveryThatGrewIsUpdated(t *testing.T) {
	s := open(t)
	u := WorkUsageRow{Room: "SG4", UsageID: "u1", BacklogID: "f-x", Card: "c1", Model: "m", StartedAt: "2026-10-15T10:00:00Z",
		EndedAt: "2026-10-15T10:01:00Z", Tokens: tokenprice.Tokens{Input: 5}}
	d := WorkUsageRow{Room: "sg4", UsageID: "d:1", BacklogID: "f-x", Card: "c1", Class: ClassCoord, Source: "delivery", Model: "m",
		StartedAt: "2026-10-15T10:00:00Z", EndedAt: "2026-10-15T10:00:00Z", Tokens: tokenprice.Tokens{Input: 3}}
	if n, err := s.AddWorkUsage([]WorkUsageRow{u, d}); err != nil || n != 2 {
		t.Fatalf("first: %d %v", n, err)
	}
	if n, _ := s.AddWorkUsage([]WorkUsageRow{u, d}); n != 0 {
		t.Fatalf("a replay wrote %d", n)
	}
	u.Input, d.Input = 99, 4
	if n, _ := s.AddWorkUsage([]WorkUsageRow{u, d}); n != 1 {
		t.Fatalf("only the delivery may change, wrote %d", n)
	}
	rows, _ := s.WorkUsage("f-x")
	if len(rows) != 2 {
		t.Fatalf("%d rows", len(rows))
	}
	for _, r := range rows {
		if r.UsageID == "u1" && (r.Input != 5 || r.Room != "sg4") {
			t.Fatalf("a usage row changed: %+v", r)
		}
		if r.UsageID == "d:1" && r.Input != 4 {
			t.Fatalf("the delivery did not grow: %+v", r)
		}
	}
}
