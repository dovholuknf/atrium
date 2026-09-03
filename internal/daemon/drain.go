package daemon

import (
	"log"

	"github.com/dovholuknf/atrium/internal/store"
)

// Turning auto mode on and leaving the queue full.
//
// Auto mode reads as "stop stopping to ask me". Every request that arrives
// after the switch is approved and never reaches the board, and the sessions
// behind them keep moving. The ones that asked a moment BEFORE the switch sit
// there, still waiting, with the header saying nothing will stop to ask.
//
// Nothing was wrong in the permission chain: it runs once per request, when
// the request arrives, and these had already run it and landed on step six.
// But an operator does not turn auto mode on in the abstract. They turn it on
// because something is waiting, and the queue not emptying is the switch
// visibly not doing the thing it says it does.
//
// So turning it on drains what is already queued. Not by writing approvals
// straight to the store, because each of those agents is parked on an
// in-memory reply channel and a decision it never sees leaves it blocked
// forever. It goes through the same decide path the buttons use.

// drainForAuto approves everything currently waiting on a human, in the order
// the chain would have answered it.
//
// The chain puts shelving and standing rules AHEAD of auto mode, so a drain
// that approved indiscriminately would mean the switch quietly overrode
// answers already given. A card can be shelved after its request was recorded,
// and a never rule can be written while a request sits in the queue, so both
// have to be checked here rather than assumed impossible.
//
// Errors on one request do not stop the rest: a queue that half emptied
// because of one bad row is worse than a queue that emptied.
func (d *Daemon) drainForAuto() (int, error) {
	pending, err := d.st.PendingPermissions()
	if err != nil {
		return 0, err
	}
	drained := 0
	for _, p := range pending {
		task, err := d.st.Get(p.TaskID)
		if err != nil {
			continue
		}
		// A standing no. Putting work down is an answer, and it outranks this.
		if task.Status == store.StatusShelved {
			continue
		}
		// A never rule is an answer already given, which auto mode does not
		// discard. Left in the queue rather than blocked here, because the
		// chain blocks on arrival and this request already got past that: the
		// rule was written afterwards, and applying it now would be answering
		// a question with a rule that did not exist when it was asked.
		rule, err := d.st.MatchRule(p.Tool, p.Command, task.Worktree)
		if err == nil && rule != nil && rule.Decision != "approve" {
			continue
		}
		if _, err := d.decide(p.ID, "approve", drainReason, ""); err != nil {
			log.Printf("[atrium] could not drain %s: %v", p.ID, err)
			continue
		}
		drained++
	}
	if drained > 0 {
		log.Printf("[atrium] auto mode approved %d request(s) that were already waiting", drained)
	}
	return drained, nil
}

// drainReason separates the queue being emptied from a request that arrived
// after the switch. Both are auto mode, but only one of them was sitting in
// front of a human who then changed their mind about being asked.
const drainReason = "global auto mode was turned on while this was waiting, so it was approved"
