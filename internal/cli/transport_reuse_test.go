package cli

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

type fakeDial struct{ wire.Dialer }

func TestReuseKeepsTheDialerForAnUnchangedPath(t *testing.T) {
	d := &fakeDial{}
	had := []link.Rung{{Path: wire.Path{Transport: "direct", Addr: "Hub.Example:7779"}, Dial: d}}
	for i := 0; i < 100; i++ {
		got := reuse(had, wire.Path{Transport: "direct", Addr: "hub.example:7779"})
		if got != wire.Dialer(d) {
			t.Fatalf("round %d: a path differing only by case got a new dialer", i)
		}
	}
	if reuse(had, wire.Path{Transport: "direct", Addr: "other:7779"}) != nil {
		t.Fatal("a different address reused a dialer")
	}
}
