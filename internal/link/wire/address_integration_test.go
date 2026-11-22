//go:build integration

package wire

import "testing"

func TestOwnRoomIsNoRoom(t *testing.T) {
	if OtherRoom("ROOM-E", "room-e") != "" || OtherRoom("", "room-e") != "" || OtherRoom("room-a", "room-e") != "room-a" {
		t.Fatal("a room part naming the caller's own room, in any case, is the same as none")
	}
}
