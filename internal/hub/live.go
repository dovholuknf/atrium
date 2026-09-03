package hub

// LiveStoreIDs is the set of durable permission ids an agent is actually
// parked on right now.
//
// The store knows which requests are unanswered. Only the hub knows which of
// those still has somebody listening for the answer: the reply channel lives
// in this process and dies with the connection that created it.
//
// The difference between the two sets is the orphans. A request whose agent
// has gone can never be answered, because there is nothing on the other end
// to hand the answer to, and a card sitting in needs-permission behind one is
// asking a question for a session that no longer exists.
func (h *Hub) LiveStoreIDs() map[string]bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	out := make(map[string]bool, len(h.pendingPerm))
	for _, p := range h.pendingPerm {
		if p.storeID != "" {
			out[p.storeID] = true
		}
	}
	return out
}
