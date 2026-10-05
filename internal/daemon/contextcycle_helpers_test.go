package daemon

// begin claims a card for a manual cycle on the limit step, for a test that only
// needs a run in flight.
func (n *newContexts) begin(taskID, file, conv string) (uint64, bool) {
	return n.claim(taskID, &newContext{step: NewContextLimit, file: file, conv: conv})
}
