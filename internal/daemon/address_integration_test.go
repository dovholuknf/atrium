//go:build integration

package daemon

import "testing"

func TestOwnRoomIsNoRoom(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks(), opts: Options{Room: "room-e"}}
	d.initServices()
	if d.otherRoom("ROOM-E") != "" || d.otherRoom("") != "" || d.otherRoom("room-a") != "room-a" {
		t.Fatal("a room part naming this room, in any case, is the same as none")
	}
}
