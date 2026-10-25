//go:build integration

package wire

import "testing"

func TestOwnRoomIsNoRoom(t *testing.T) {
	if OtherRoom("SG4", "sg4") != "" || OtherRoom("", "sg4") != "" || OtherRoom("m1mini", "sg4") != "m1mini" {
		t.Fatal("a room part naming the caller's own room, in any case, is the same as none")
	}
}
