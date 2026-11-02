//go:build integration

package daemon

import "testing"

func TestOwnRoomIsNoRoom(t *testing.T) {
	t.Parallel()
	d := &Daemon{hk: *testHooks(), opts: Options{Room: "sg4"}}
	d.initServices()
	if d.otherRoom("SG4") != "" || d.otherRoom("") != "" || d.otherRoom("m1mini") != "m1mini" {
		t.Fatal("a room part naming this room, in any case, is the same as none")
	}
}
