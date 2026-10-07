package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// replyLine appends an assistant line with content blocks, as Claude Code writes a reply in several.
func (f *usageFix) replyLine(at time.Time, id string, out int64, blocks ...map[string]any) {
	f.t.Helper()
	b, _ := json.Marshal(map[string]any{
		"type": "assistant", "timestamp": at.Format(time.RFC3339Nano),
		"message": map[string]any{"id": id, "model": "claude-sonnet-5-5", "content": blocks,
			"usage": map[string]any{"input_tokens": 3, "cache_creation_input_tokens": 100, "cache_read_input_tokens": 1000,
				"output_tokens": out}},
	})
	fh, err := os.OpenFile(f.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		f.t.Fatal(err)
	}
	defer fh.Close()
	fmt.Fprintln(fh, string(b))
}

func (f *usageFix) reportOf() *store.CostReport {
	f.t.Helper()
	rep, err := f.st.DeliveryCosts(f.base.Add(-time.Hour*24*400), time.Now().Add(time.Hour), 10)
	if err != nil {
		f.t.Fatal(err)
	}
	return rep
}

// The turn after a delivery is read at the Stop and costed on it: tokens from the replies once per message id, an
// estimated dollar figure, and the reply an ack. A turn that used a tool is work.
func TestTurnCostLandsOnTheDelivery(t *testing.T) {
	f := newUsageFix(t)
	f.u.launched(f.task.ID, false)
	text := func(s string) map[string]any { return map[string]any{"type": "text", "text": s} }
	tool := map[string]any{"type": "tool_use", "name": "Read"}

	f.noteDelivery(store.DeliveryNudge, f.base.Add(-time.Second))
	f.replyLine(f.base, "m1", 10, text("Got it."))
	f.replyLine(f.base, "m1", 10, text(" Thanks."))
	f.u.prompted(f.task.ID, store.UsageSay)
	f.record(usageSegment{cause: store.UsageSay, stop: f.base.Add(time.Minute)})

	f.noteDelivery(store.DeliverySay, f.base.Add(90*time.Second))
	f.replyLine(f.base.Add(2*time.Minute), "m2", 50, text("looking"), tool)
	f.record(usageSegment{cause: store.UsageSay, stop: f.base.Add(3 * time.Minute)})

	rep := f.reportOf()
	got := map[string]store.CostRow{}
	for _, r := range rep.Rows {
		got[r.Kind] = r
	}
	n := got[store.DeliveryNudge]
	// each reply is 3 input, 100 written, 10 out, 1000 read, counted once
	if n.Turns != 1 || n.Input != 3 || n.Output != 10 || n.CacheWrite != 100 || n.CacheRead != 1000 || n.AckTurns != 1 || n.NoiseTurns != 1 {
		t.Fatalf("nudge %+v", n)
	}
	if n.Cost <= 0 {
		t.Fatalf("no estimate for a priced model: %+v", n)
	}
	s := got[store.DeliverySay]
	if s.Turns != 1 || s.NoiseTurns != 0 || s.Output != 50 {
		t.Fatalf("say %+v", s)
	}
	if len(rep.Top) != 2 || rep.Top[0].Output != 50 {
		t.Fatalf("top %+v", rep.Top)
	}
}

func (f *usageFix) noteDelivery(kind string, at time.Time) {
	f.t.Helper()
	if err := f.st.RecordDelivery(&store.Delivery{TaskID: f.task.ID, Kind: kind, At: at}); err != nil {
		f.t.Fatal(err)
	}
}
