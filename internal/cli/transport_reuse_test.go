package cli

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/link"
)

type fakeDial struct{ link.Dialer }

func TestReuseKeepsTheDialerForAnUnchangedPath(t *testing.T) {
	d := &fakeDial{}
	had := []link.Rung{{Path: link.Path{Transport: "direct", Addr: "Hub.Example:7779"}, Dial: d}}
	for i := 0; i < 100; i++ {
		got := reuse(had, link.Path{Transport: "direct", Addr: "hub.example:7779"})
		if got != link.Dialer(d) {
			t.Fatalf("round %d: a path differing only by case got a new dialer", i)
		}
	}
	if reuse(had, link.Path{Transport: "direct", Addr: "other:7779"}) != nil {
		t.Fatal("a different address reused a dialer")
	}
}
