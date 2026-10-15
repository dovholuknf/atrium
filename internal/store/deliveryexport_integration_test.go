//go:build integration

package store

import (
	"testing"
	"time"
)

// THE HUB READS COSTED DELIVERIES with the sender resolved to a card, and the carrier of a turn apart from the ones that
// share it. A delivery not yet costed is not in it.
func TestDeliveryExportResolvesTheSenderAndMarksTheCarrier(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	orch, _, err := s.Register(Observed{WireName: "orch", Worktree: "D:/w/orch", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	worker, _, err := s.Register(Observed{WireName: "w-2", Worktree: "D:/w/w2", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for _, from := range []string{"w-2", "nobody"} {
		if err := s.RecordDelivery(&Delivery{TaskID: orch.ID, From: from, Kind: DeliveryReport}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.CostDeliveries(orch.ID, time.Now().Add(time.Minute), TurnCost{Model: "claude-opus-5-5", Input: 40, Output: 7}); err != nil {
		t.Fatal(err)
	}
	if err := s.RecordDelivery(&Delivery{TaskID: orch.ID, From: "w-2", Kind: DeliveryReport, At: time.Now().Add(time.Hour)}); err != nil {
		t.Fatal(err)
	}
	var got []DeliveryExport
	if err := s.DeliveriesSince(time.Now().Add(-time.Hour), func(d *DeliveryExport) error { got = append(got, *d); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("%d costed deliveries, want 2: %+v", len(got), got)
	}
	var carrier, shared *DeliveryExport
	for i := range got {
		if got[i].Group == "" {
			carrier = &got[i]
		} else {
			shared = &got[i]
		}
	}
	if carrier == nil || shared == nil || shared.Group != carrier.ID || carrier.Input != 40 || carrier.Output != 7 || shared.Input != 0 {
		t.Fatalf("carrier %+v shared %+v", carrier, shared)
	}
	if carrier.TaskID != orch.ID {
		t.Fatalf("recipient = %s", carrier.TaskID)
	}
	for _, d := range got {
		if d.From == "w-2" && d.FromTask != worker.ID {
			t.Fatalf("sender not resolved: %+v", d)
		}
		if d.From == "nobody" && d.FromTask != "" {
			t.Fatalf("an unknown sender resolved to %s", d.FromTask)
		}
	}
}
