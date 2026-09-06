package daemon

import (
	"testing"
	"time"
)

// The hub holds nothing durable about a room, which is the whole design. Every
// test here is about that: what the hub forgets, and when.

func roomsOf(t *testing.T, d *Daemon) []Room {
	t.Helper()
	got, ok := d.Rooms().(map[string]any)
	if !ok {
		t.Fatalf("Rooms answered %T", d.Rooms())
	}
	list, ok := got["rooms"].([]Room)
	if !ok {
		t.Fatalf("rooms is %T", got["rooms"])
	}
	return list
}

// A room's cards are REPLACED on every check-in, never merged. A merge would
// mean a card deleted on the room lives forever on the hub, and the point of
// holding nothing durable is that the room is the truth about itself.
func TestACheckInReplacesTheCards(t *testing.T) {
	d := &Daemon{}
	if err := d.RoomCheckIn(RoomReport{Name: "one", Cards: []RoomCard{
		{ID: "a", Title: "first"}, {ID: "b", Title: "second"},
	}}); err != nil {
		t.Fatal(err)
	}
	if err := d.RoomCheckIn(RoomReport{Name: "one", Cards: []RoomCard{
		{ID: "b", Title: "second"},
	}}); err != nil {
		t.Fatal(err)
	}
	list := roomsOf(t, d)
	if len(list) != 1 {
		t.Fatalf("%d rooms", len(list))
	}
	if len(list[0].Cards) != 1 || list[0].Cards[0].ID != "b" {
		t.Fatalf("a card deleted on the room survived on the hub: %+v", list[0].Cards)
	}
}

// A room that stops talking is SAID to be stale rather than dropped. A machine
// that died and one that was never there look the same otherwise, and the first
// is the one worth noticing.
func TestAQuietRoomGoesStaleBeforeItGoes(t *testing.T) {
	d := &Daemon{}
	if err := d.RoomCheckIn(RoomReport{Name: "quiet"}); err != nil {
		t.Fatal(err)
	}
	if roomsOf(t, d)[0].Stale {
		t.Fatal("a room that just checked in was reported stale")
	}

	// Reach in and age it, rather than sleeping for a minute.
	d.rooms.mu.Lock()
	d.rooms.all["quiet"].LastSeen = time.Now().Add(-roomStale - time.Second)
	d.rooms.mu.Unlock()

	list := roomsOf(t, d)
	if len(list) != 1 {
		t.Fatalf("a stale room was dropped rather than reported: %d rooms", len(list))
	}
	if !list[0].Stale {
		t.Fatal("a room that has missed its heartbeats is not reported stale")
	}
}

// And a room nobody has heard from in long enough disappears, because listing
// it is a claim rather than a memory.
func TestAForgottenRoomIsGone(t *testing.T) {
	d := &Daemon{}
	if err := d.RoomCheckIn(RoomReport{Name: "gone"}); err != nil {
		t.Fatal(err)
	}
	d.rooms.mu.Lock()
	d.rooms.all["gone"].LastSeen = time.Now().Add(-roomForget - time.Second)
	d.rooms.mu.Unlock()

	if list := roomsOf(t, d); len(list) != 0 {
		t.Fatalf("a room nobody has heard from in %s is still listed: %+v", roomForget, list)
	}
}

// The waiting count is what makes a remote room actionable rather than
// informational, and it is computed on read because it is a fact about now.
func TestWaitingIsCountedFromTheCards(t *testing.T) {
	d := &Daemon{}
	if err := d.RoomCheckIn(RoomReport{Name: "busy", Cards: []RoomCard{
		{ID: "a", Status: "running"},
		{ID: "b", Status: "needs-input"},
		{ID: "c", Status: "needs-permission"},
		{ID: "d", Status: "done"},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := roomsOf(t, d)[0].Waiting; got != 2 {
		t.Fatalf("waiting is %d, wanted 2", got)
	}
}

// A room has to say what it is called, because the name is the key. Two rooms
// with one name are one room that flaps, and an unnamed one would be every
// room at once.
func TestARoomHasToBeNamed(t *testing.T) {
	d := &Daemon{}
	if err := d.RoomCheckIn(RoomReport{Name: "  "}); err == nil {
		t.Fatal("a room with no name was accepted")
	}
	if err := d.RoomCheckIn(RoomReport{Name: string(make([]byte, 200))}); err == nil {
		t.Fatal("a room name too long to draw was accepted")
	}
}

// Checking in twice is one room, not two. The second is an update.
func TestCheckingInTwiceIsOneRoom(t *testing.T) {
	d := &Daemon{}
	for i := 0; i < 5; i++ {
		if err := d.RoomCheckIn(RoomReport{Name: "same", Host: "h"}); err != nil {
			t.Fatal(err)
		}
	}
	if list := roomsOf(t, d); len(list) != 1 {
		t.Fatalf("five check-ins produced %d rooms", len(list))
	}
}
