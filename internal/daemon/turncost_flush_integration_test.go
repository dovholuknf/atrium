//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A usage flush in the middle of a turn costs the delivery with what it has read so far. The Stop that ends the turn
// reads only what came after, so it adds that to the same delivery: the turn is costed whole, and counted once.
func TestTurnCostOfAMidTurnFlushIsTheWholeTurn(t *testing.T) {
	f := newUsageFix(t)
	f.u.launched(f.task.ID, false)
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }

	f.noteDelivery(store.DeliverySay, f.base.Add(-time.Second))
	f.replyLine(f.base, "m1", 10, text("working"))
	f.record(usageSegment{cause: store.UsageSay, stop: f.base.Add(time.Minute), flush: true})
	if rep := f.reportOf(); len(rep.Rows) != 1 || rep.Rows[0].Output != 10 {
		t.Fatalf("the flush costed %+v", rep.Rows)
	}

	f.replyLine(f.base.Add(2*time.Minute), "m2", 20, text("done"))
	f.record(usageSegment{cause: store.UsageSay, stop: f.base.Add(3 * time.Minute)})

	rep := f.reportOf()
	if len(rep.Rows) != 1 {
		t.Fatalf("rows %+v", rep.Rows)
	}
	r := rep.Rows[0]
	if r.Turns != 1 || r.Output != 30 || r.Input != 6 || r.CacheWrite != 200 {
		t.Fatalf("the turn was costed %+v, want one turn of both replies", r)
	}
	if len(rep.Top) != 1 || rep.Top[0].Replies != 2 || rep.Top[0].Reply != store.ReplyText {
		t.Fatalf("top %+v", rep.Top)
	}

	// The turn is over: a Stop with nothing more to read changes nothing.
	f.record(usageSegment{cause: store.UsageSay, stop: f.base.Add(4 * time.Minute)})
	if r2 := f.reportOf().Rows[0]; r2.Output != 30 {
		t.Fatalf("a later read changed the turn: %+v", r2)
	}
}
