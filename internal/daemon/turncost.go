package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"log"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// WHAT A TURN COST, BY WHAT CAUSED IT. Every text atrium puts in front of a session is a delivery (store/delivery.go)
// with its kind, the card it came from and the card it went to. The Stop that ends the turn after it already reads
// the turn's replies off the transcript, one per message id, for the usage row. The same read lands here: the turn's
// tokens, an estimated dollar figure from the price table, and what the reply was, on every delivery the card had
// waiting.
//
// BEST EFFORT, like the usage row it rides on. A hook never fails a session, and this runs off the Stop's goroutine
// after the settle delay, so it cannot hold one either. A failure is logged and the delivery stays uncosted for the
// next turn.
//
// THE DOLLAR FIGURE IS AN ESTIMATE. It is each reply priced on its own model from usageOnlyPrices and
// keepalivePrices, which are list prices. A model with no price is counted at zero, and its tokens still show.

// noteDelivery records a text atrium put in front of a session without queueing it: one it typed straight in. A
// queued message is recorded where it is marked delivered. Best effort.
func (d *Daemon) noteDelivery(taskID, from, kind string) {
	if d == nil || d.st == nil || taskID == "" {
		return
	}
	if err := d.st.RecordDelivery(&store.Delivery{TaskID: taskID, From: from, Kind: kind}); err != nil {
		log.Printf("[atrium] could not record a %s delivery to %s: %v", kind, taskID, err)
	}
}

// bannerKind is the kind of a delivery typed in under this label.
func bannerKind(banner string) string {
	switch banner {
	case "":
		return store.DeliveryOperator
	case cycle.WakeLabel, exitLabel:
		return store.DeliveryRestartWake
	}
	return store.DeliveryCycle
}

// turnCarry is a turn a mid-turn flush costed before the turn was over: the delivery that carries it.
type turnCarry struct {
	delivery string
	// text and tool are what the turn said and whether it used a tool so far, so the reply class is the whole turn's.
	text string
	tool bool
}

// costDeliveries lands a turn's cost on the card's deliveries. main is the replies the usage row just counted.
//
// A FLUSH IS NOT A STOP. A report, an exit or a runner going reads the transcript while the turn may still run, and
// the Stop that ends it reads only what came after (the cursor sees to that). Costing on the flush alone would leave
// the delivery with a part of the turn. So a flush costs what it read and remembers the delivery, and the next read
// that finds nothing new waiting adds its replies to that same delivery. A delivery that arrived in between starts
// its own turn and takes the later replies itself. A flush stays costed even if no Stop follows, which is the case
// for a launched worker with no Stop hook.
//
// Called with the tracker's read lock held, which is what guards the carry.
func (u *usageTracker) costDeliveries(t *store.Task, seg usageSegment, main *replySet) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[atrium] could not cost the turn on %s: %v", t.ID, r)
		}
	}()
	c := turnCostOf(main)
	text, tool := replyTextOf(main)
	n, first, err := u.st.CostDeliveriesFirst(t.ID, seg.stop, c)
	if err != nil {
		log.Printf("[atrium] could not cost the turn on %s: %v", t.ID, err)
		return
	}
	prev := u.carry[t.ID]
	if n > 0 {
		// A new cause: the earlier carry is a turn that is over.
		delete(u.carry, t.ID)
		if seg.flush {
			u.carry[t.ID] = &turnCarry{delivery: first, text: text, tool: tool}
		}
		return
	}
	if prev == nil {
		return
	}
	prev.text, prev.tool = prev.text+text, prev.tool || tool
	c.Reply = store.ClassifyReply(prev.text, prev.tool)
	if err := u.st.AddToDeliveryCost(prev.delivery, c); err != nil {
		log.Printf("[atrium] could not finish costing the turn on %s: %v", t.ID, err)
	}
	if !seg.flush {
		delete(u.carry, t.ID)
	}
}

// replyTextOf is what a set of replies said, and whether any used a tool.
func replyTextOf(set *replySet) (string, bool) {
	var (
		text strings.Builder
		tool bool
	)
	for _, id := range set.order {
		r := set.byID[id]
		text.WriteString(r.Text)
		tool = tool || r.ToolUse
	}
	return text.String(), tool
}

// turnCostOf sums a set of replies into what the turn spent, each priced on its own model.
func turnCostOf(set *replySet) store.TurnCost {
	var (
		c    store.TurnCost
		text strings.Builder
		tool bool
	)
	for _, id := range set.order {
		r := set.byID[id]
		w5, w1 := r.Write5m, r.Write1h
		if w5+w1 < r.CacheWrite {
			w5 += r.CacheWrite - w5 - w1
		}
		one := &store.SessionUsage{Input: r.Input, Output: r.Output, CacheRead: r.CacheRead, CacheWrite5m: w5, CacheWrite1h: w1}
		c.Input += one.Input
		c.Output += one.Output
		c.CacheRead += one.CacheRead
		c.CacheWrite5m += w5
		c.CacheWrite1h += w1
		if p, ok := usagePriceFor(r.Model); ok {
			c.Cost += usageCost(one, p)
		}
		c.Model = r.Model
		text.WriteString(r.Text)
		tool = tool || r.ToolUse
	}
	c.Replies = len(set.order)
	c.Reply = store.ClassifyReply(text.String(), tool)
	return c
}
