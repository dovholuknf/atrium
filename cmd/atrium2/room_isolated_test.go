package main

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
