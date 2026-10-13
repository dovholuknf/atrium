//go:build integration

package cli

import (
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A HALTED HUB STILL KNOWS ITS ROOMS. f-001: the store halted, and the heartbeat's re-check of every attached room
// read the halt as "this hub has no room called X", so every room was let go and none could come back. A room the
// hub knew before the halt stays attached and may reattach. One it never knew is still refused.
func TestAHaltedHubKeepsTheRoomsItKnew(t *testing.T) {
	store, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.Add("sparta", hubstore.TransportDirect); err != nil {
		t.Fatal(err)
	}
	rooms := newHubRooms(store)
	if err := rooms.attaching("sparta", "h", "v"); err != nil {
		t.Fatalf("a known room was refused on a healthy hub: %v", err)
	}
	if err := rooms.attaching("athens", "h", "v"); err == nil {
		t.Fatal("a room the hub never made attached")
	}

	// The database going away under a running hub. The next call halts the store.
	store.Close()
	if _, err := store.Rooms(); err == nil {
		t.Fatal("a closed store still answered")
	}
	if halted, _ := store.Halted(); !halted {
		t.Fatal("the store did not halt")
	}

	// The heartbeat, and a room coming back, both ask this.
	if err := rooms.attaching("sparta", "h", "v"); err != nil {
		t.Fatalf("a room the hub knew was let go because the store halted: %v", err)
	}
	if err := rooms.attaching("athens", "h", "v"); err == nil {
		t.Fatal("a halted hub let in a room it never knew")
	}
}
