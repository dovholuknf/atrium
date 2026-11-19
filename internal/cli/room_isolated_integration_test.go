//go:build integration

package cli

import (
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// --isolated keeps a throwaway room off the machine's shared pointer.
//
// The incident this guards against: a second room started with only port and dir
// overrides still defaults its address to the FIXED shared path, overwrites it,
// and from then on the first room's hooks arrive at the throwaway. The flag must
// send the address to a private file beside --dir and never write the shared one.
func TestIsolatedRoomWritesAPrivateAddressAndLeavesTheSharedOneAlone(t *testing.T) {
	dir := t.TempDir()

	location, shared := roomLocationEnv(true, dir)

	want := filepath.Join(dir, "daemon.json")
	if location != want {
		t.Fatalf("isolated room records its address at %q, wanted the private %q", location, want)
	}
	if shared != "-" {
		t.Fatalf("isolated room would publish a shared copy at %q, wanted none (%q)", shared, "-")
	}

	// And prove internal/daemon reads those values the way the room means them:
	// the private file is the location, and `-` writes no shared file at all.
	t.Setenv("ATRIUM_LOCATION", location)
	t.Setenv("ATRIUM_SHARED_LOCATION", shared)

	got, err := daemon.LocationPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("the daemon would write its address to %q, wanted the private %q", got, want)
	}
	if p := daemon.SharedLocationPath(); p != "" {
		t.Fatalf("the daemon would still write a shared address at %q, taking over the machine's hooks", p)
	}
}

// An isolated `atrium run` beside the machine's own: with only its hub's addresses given, its room takes a private
// directory, database and ports, and probes its own address file rather than the machine's.
func TestIsolatedRunStartsItsOwnRoomBesideTheMachinesRoom(t *testing.T) {
	hub := t.TempDir()
	machine := roomLaunch{human: defaultRoomHTTP(), agent: defaultRoomAgent()}
	own := isolatedRoomLaunch(machine, atriumFlags{board: "127.0.0.1:7878", dir: hub}, false, false)

	if own.dir == roomDir() || own.dir != filepath.ToSlash(filepath.Join(hub, "room")) {
		t.Fatalf("isolated room keeps its keys in %q, wanted a directory under %q", own.dir, hub)
	}
	if own.db == defaultRoomDB() || own.db != own.dir+"/atrium.db" {
		t.Fatalf("isolated room's database is %q, wanted one beside its keys", own.db)
	}
	if own.human != "127.0.0.1:7881" || own.agent != "127.0.0.1:7877" {
		t.Fatalf("isolated room listens on %q and %q, wanted 7881 and 7877", own.human, own.agent)
	}
	given := isolatedRoomLaunch(roomLaunch{human: "127.0.0.1:9001", agent: "127.0.0.1:9002"},
		atriumFlags{board: "127.0.0.1:7878", dir: hub}, true, true)
	if given.human != "127.0.0.1:9001" || given.agent != "127.0.0.1:9002" {
		t.Fatalf("flags given were overridden: %q %q", given.human, given.agent)
	}
	own.isolated = true
	t.Setenv("ATRIUM_LOCATION", filepath.Join(t.TempDir(), "machine.json"))
	if got, want := roomAddressFile(own), filepath.Join(own.dir, "daemon.json"); got != want {
		t.Fatalf("isolated run probes %q, wanted its own %q", got, want)
	}
}

// The default room is unchanged: it keeps using the fixed shared path so hooks
// find it without being told its dir. Only --isolated moves the address.
func TestTheDefaultRoomStillUsesTheFixedPath(t *testing.T) {
	location, shared := roomLocationEnv(false, t.TempDir())

	if location != roomLocation() {
		t.Fatalf("a normal room recorded its address at %q, wanted the fixed %q", location, roomLocation())
	}
	if shared != "-" {
		t.Fatalf("a room published a shared copy at %q, wanted none (%q)", shared, "-")
	}
}
