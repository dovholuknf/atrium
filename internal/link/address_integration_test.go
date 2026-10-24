//go:build integration

package link

import "testing"

func TestOwnRoomIsNoRoom(t *testing.T) {
	if otherRoom("SG4", "sg4") != "" || otherRoom("", "sg4") != "" || otherRoom("m1mini", "sg4") != "m1mini" {
		t.Fatal("a room part naming the caller's own room, in any case, is the same as none")
	}
}
