//go:build integration

package store

import (
	"testing"
	"time"
)

// A message that reaches a session is a delivery, the turn after it is costed once however many came, and the report
// leaves the operator's out of the top and the noise.
func TestDeliveryCostedAtStop(t *testing.T) {
	s := openTestStore(t)
	task, _, err := s.Register(Observed{WireName: "target", Worktree: "D:/w/target", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	q := func(text, from, cause string) *Message {
		t.Helper()
		m, err := s.QueueCaused(task.ID, text, from, "", cause, false)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}
	a := q("ended without a report", "atrium", DeliveryNudge)
	b := q("hello", "w-2", "")
	if err := s.MarkDelivered(task.ID, "stop", []string{a.ID, b.ID}); err != nil {
		t.Fatal(err)
	}
	// the operator's, typed at the terminal and carried: left to the prompt hook
	op, err := s.QueueMessage(task.ID, "do the thing")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.MarkDelivered(task.ID, "terminal", []string{op.ID}); err != nil {
		t.Fatal(err)
	}
	s.RecordDelivery(&Delivery{TaskID: task.ID, Kind: DeliveryOperator})

	n, err := s.CostDeliveries(task.ID, time.Now().Add(time.Minute), TurnCost{
		Model: "claude-sonnet-5-5", Replies: 2, Input: 10, Output: 5, CacheWrite5m: 100, CacheRead: 1000, Cost: 0.5, Reply: ReplyAck})
	if err != nil || n != 3 {
		t.Fatalf("costed %d, %v", n, err)
	}
	// nothing is left to cost twice
	if n, _ := s.CostDeliveries(task.ID, time.Now().Add(time.Minute), TurnCost{Input: 99}); n != 0 {
		t.Fatalf("costed again: %d", n)
	}
	rep, err := s.DeliveryCosts(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), 10)
	if err != nil {
		t.Fatal(err)
	}
	kinds := map[string]CostRow{}
	var tokens int64
	for _, r := range rep.Rows {
		kinds[r.Kind] = r
		tokens += r.Input + r.Output + r.CacheWrite
	}
	if len(kinds) != 3 || kinds[DeliveryNudge].Kind == "" || kinds[DeliverySay].Kind == "" || kinds[DeliveryOperator].Kind == "" {
		t.Fatalf("kinds %+v", kinds)
	}
	// 115 once: the first delivery carried it, the others shared it.
	if tokens != 115 {
		t.Fatalf("tokens %d, want 115 counted once", tokens)
	}
	if k := kinds[DeliveryNudge]; k.Turns != 1 || k.NoiseTurns != 1 || k.AckTurns != 1 || k.Cost != 0.5 {
		t.Fatalf("nudge %+v", k)
	}
	if k := kinds[DeliverySay]; k.Turns != 0 || k.DeliveryRows != 1 {
		t.Fatalf("say shared the turn %+v", k)
	}
	if len(rep.Top) != 1 || rep.Top[0].Kind != DeliveryNudge || rep.Top[0].Reply != ReplyAck {
		t.Fatalf("top %+v", rep.Top)
	}
}
