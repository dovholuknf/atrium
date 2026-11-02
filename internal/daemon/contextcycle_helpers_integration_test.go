//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
)

// begin claims a card for a manual cycle on the limit step, for a test that only
// needs a run in flight.
func beginCycle(n *cycle.NewContexts, taskID, file, conv string) (uint64, bool) {
	return n.Claim(taskID, &cycle.NewContext{Step: cycle.NewContextLimit, File: file, Conv: conv, Armed: true})
}
