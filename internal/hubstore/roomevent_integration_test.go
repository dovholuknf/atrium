//go:build integration

package hubstore

import "testing"

func TestRoomEventIsTakenOncePerRoomAndSeq(t *testing.T) {
	s := open(t)
	if seen, _ := s.RoomEventSeen("M1", 1); seen {
		t.Fatal("a fresh hub has seen nothing")
	}
	for i := 0; i < 2; i++ {
		if err := s.RoomEventTake("M1", 1, "merge.ask", "", true); err != nil {
			t.Fatal(err)
		}
	}
	if seen, _ := s.RoomEventSeen("m1", 1); !seen {
		t.Fatal("a taken event is seen, whatever the case of the room")
	}
	if seen, _ := s.RoomEventSeen("m1", 2); seen {
		t.Fatal("seq 2 was not taken")
	}
	if seen, _ := s.RoomEventSeen("room-d", 1); seen {
		t.Fatal("seq 1 of another room was not taken")
	}
	if high, _ := s.RoomEventHigh("m1"); high != 1 {
		t.Fatalf("high %d", high)
	}
}
