package cli

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link"
)

// The room resolves an item's owner (internal/daemon) and the hub checks it again (internal/hubstore). The room
// cannot import the hub's store, so this is what keeps the two saying the same thing.
func TestTheRoomAndTheHubResolveAnItemOwnerAlike(t *testing.T) {
	if link.ItemOwnerTag != "atrium:item-owner:" {
		t.Fatalf("the owner tag moved: %q", link.ItemOwnerTag)
	}
	for _, c := range []struct{ spec, creator string }{
		{"", "orchestrator"}, {"merger", "orchestrator"}, {"role:director", "x"}, {"orchestrator", "x"},
		{"human", "orchestrator"}, {"tester", "orchestrator"}, {"role:tester", "x"}, {"boss", "x"}, {" Merger ", "x"},
	} {
		hk, ho, herr := hubstore.ResolveOwner(c.spec, c.creator)
		rk, ro, rerr := daemon.ResolveItemOwner(c.spec, c.creator)
		if (herr != nil) != (rerr != nil) || hk != rk || ho != ro {
			t.Errorf("%q by %q: hub %q %q %v, room %q %q %v", c.spec, c.creator, hk, ho, herr, rk, ro, rerr)
		}
	}
}
