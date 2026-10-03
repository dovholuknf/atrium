package store

import (
	"errors"
	"testing"
)

func TestTheAtriumHandleAndAliasAreReserved(t *testing.T) {
	s := openTestStore(t)
	if _, _, err := s.Register(Observed{WireName: "atrium", Runner: "claude"}); !errors.Is(err, ErrReservedName) {
		t.Fatalf("a told atrium: %v", err)
	}
	other := claimCard(t, s, "somebody")
	for _, a := range []string{"atrium", "@Atrium"} {
		if err := s.SetAlias(other.ID, a); !errors.Is(err, ErrReservedName) {
			t.Fatalf("alias %q: %v", a, err)
		}
	}
	// A folder called atrium is this repository's own. Kept, under another name, the same each time.
	a, _, err := s.Register(Observed{WireName: "atrium", NameSource: NameFromDir, Worktree: "/w/atrium", Runner: "claude"})
	if err != nil || a.WireName != ReservedDerived {
		t.Fatalf("a folder-derived atrium: %v %v", a, err)
	}
	b, created, err := s.Register(Observed{WireName: "atrium", NameSource: NameFromDir, Worktree: "/w/atrium", Runner: "claude"})
	if err != nil || created || b.ID != a.ID {
		t.Fatalf("the derived name did not find its card again: %v %v %v", b, created, err)
	}
}

func TestAnOwedItemSurvivesAndClosesOnce(t *testing.T) {
	s := openTestStore(t)
	if err := s.PutOwedItem(OwedItem{Worker: "w1", Host: "h1"}); err != nil {
		t.Fatal(err)
	}
	items, _ := s.OpenOwedItems()
	if len(items) != 1 {
		t.Fatalf("%d open", len(items))
	}
	if closed, _ := s.CloseOwedItem("w1"); !closed {
		t.Fatal("not closed")
	}
	if closed, _ := s.CloseOwedItem("w1"); closed {
		t.Fatal("closed twice")
	}
	if items, _ := s.OpenOwedItems(); len(items) != 0 {
		t.Fatalf("%d open after close", len(items))
	}
	if s.OwedClosedAt("w1").IsZero() {
		t.Fatal("no close stamp")
	}
}
