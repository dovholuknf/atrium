package gitsync

import "testing"

// A collect that finds a push from the room in flight is skipped and owed, and a push of another room is not in its way.
func TestACollectWaitsForAPushFromTheSameRoom(t *testing.T) {
	h := &Hub{}
	if h.pushInFlight("SG3") {
		t.Fatal("a room with no push in flight was held")
	}
	end := h.pushBegan("sg3")
	if !h.pushInFlight("SG3") {
		t.Fatal("a room with a push in flight was not held")
	}
	if h.pushInFlight("m1mini") {
		t.Fatal("another room's push held this room")
	}
	if !h.owed["sg3"] {
		t.Fatal("the skipped collect was not owed")
	}
	h.mu.Lock()
	h.owed["sg3"] = false
	h.mu.Unlock()
	end()
	if h.pushInFlight("sg3") {
		t.Fatal("the room was still held after its push ended")
	}
}
