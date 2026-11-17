//go:build integration

package store

import "testing"

func TestRoomEventsAreNumberedInOrderAndStayAfterTheyAreSent(t *testing.T) {
	s := open(t)
	for i := 0; i < 3; i++ {
		if _, err := s.RoomEventAppend(RoomEvent{Kind: RoomEventMergeAsk, Method: "POST", Path: "/_hub/merge"}); err != nil {
			t.Fatal(err)
		}
	}
	rows, _ := s.RoomEventsAfter(0, 10)
	if len(rows) != 3 || rows[0].Seq != 1 || rows[2].Seq != 3 {
		t.Fatalf("rows: %+v", rows)
	}
	if n, oldest, _ := s.RoomEventsHeld(); n != 3 || oldest == "" {
		t.Fatalf("held %d at %q", n, oldest)
	}
	_ = s.RoomEventFailed(1, "down")
	_ = s.RoomEventSent(1, "the hub refused it: no merge room")
	if n, _, _ := s.RoomEventsHeld(); n != 2 {
		t.Fatalf("after one sent, %d held", n)
	}
	rows, _ = s.RoomEventsAfter(0, 10)
	if len(rows) != 3 || rows[0].SentAt == "" || rows[0].LastError != "the hub refused it: no merge room" {
		t.Fatalf("a sent row stays in the log with its note: %+v", rows[0])
	}
	if rows, _ = s.RoomEventsAfter(2, 10); len(rows) != 1 || rows[0].Seq != 3 {
		t.Fatalf("after 2: %+v", rows)
	}
	next, _ := s.RoomEventAppend(RoomEvent{Kind: RoomEventMergeAsk, Method: "POST", Path: "/_hub/merge"})
	if max, _ := s.RoomEventMax(); next != 4 || max != 4 {
		t.Fatalf("a sent row freed its number: next %d max %d", next, max)
	}
}

func TestRoomEventSetBodyGivesAFilingItsId(t *testing.T) {
	s := open(t)
	seq, _ := s.RoomEventAppend(RoomEvent{Kind: RoomEventBacklogFile, Method: "POST", Path: "/_hub/backlog", Body: "{}"})
	if err := s.RoomEventSetBody(seq, "m1-1", `{"id":"m1-1","upsert":true}`); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.RoomEventsAfter(0, 10)
	if len(rows) != 1 || rows[0].Key != "m1-1" || rows[0].Body != `{"id":"m1-1","upsert":true}` {
		t.Fatalf("%+v", rows)
	}
}
