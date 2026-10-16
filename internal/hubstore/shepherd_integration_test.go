//go:build integration

package hubstore

import (
	"errors"
	"strings"
	"testing"
)

func TestAShepherdRowIsWrittenWholeAndReadBack(t *testing.T) {
	s := open(t)
	if _, err := s.ShepherdGet("bug-x"); !errors.Is(err, ErrNoShepherd) {
		t.Fatalf("empty store: %v", err)
	}
	first, err := s.ShepherdPut(Shepherd{ItemID: "bug-x", Phase: "check", BaseSHA: "abc"})
	if err != nil {
		t.Fatal(err)
	}
	next := first
	next.Phase, next.Card, next.LastError = "wait", "card-1", "boom"
	got, err := s.ShepherdPut(next)
	if err != nil || got.Phase != "wait" || got.Card != "card-1" || got.BaseSHA != "abc" || got.LastError != "boom" {
		t.Fatalf("%+v %v", got, err)
	}
	if got.CreatedAt != first.CreatedAt {
		t.Errorf("created_at moved: %s then %s", first.CreatedAt, got.CreatedAt)
	}
}

func TestAShepherdRowNeedsAnItemAndAPhaseAndIsBounded(t *testing.T) {
	s := open(t)
	if _, err := s.ShepherdPut(Shepherd{Phase: "check"}); !IsRefusal(err) {
		t.Errorf("no item: %v", err)
	}
	if _, err := s.ShepherdPut(Shepherd{ItemID: "x"}); !IsRefusal(err) {
		t.Errorf("no phase: %v", err)
	}
	got, err := s.ShepherdPut(Shepherd{ItemID: "x", Phase: "held", Packet: strings.Repeat("a", ShepherdMax+10)})
	if err != nil || len(got.Packet) != ShepherdMax {
		t.Fatalf("packet %d %v", len(got.Packet), err)
	}
}
