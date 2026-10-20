//go:build integration

package cli

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
)

// A HALTED HUB STILL KNOWS ITS ROOMS. f-001: the store halted, and the heartbeat's re-check of every attached room
// read the halt as "this hub has no room called X", so every room was let go and none could come back. A room the
// hub knew before the halt stays attached and may reattach. One it never knew is still refused.
func TestAHaltedHubKeepsTheRoomsItKnew(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	store, err := hubstore.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
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
	breakRoomTable(t, path)
	if _, err := store.Rooms(); err == nil {
		t.Fatal("a broken store still answered")
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

func breakRoomTable(t *testing.T, path string) {
	t.Helper()
	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec("ALTER TABLE room RENAME TO room_gone"); err != nil {
		t.Fatal(err)
	}
}
