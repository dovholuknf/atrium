//go:build integration

package store

import (
	"path/filepath"
	"testing"
)

func TestReviewMigrationIsRecordedAndReopensClean(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReviewStart("c1", "main", "abc", "[]"); err != nil {
		t.Fatal(err)
	}
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer s.Close()
	if r, err := s.ReviewOf("c1"); err != nil || r.Base != "main" {
		t.Fatalf("%+v %v", r, err)
	}
	if err := s.ReviewStart("c1", "dev", "def", "[]"); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.ReviewOf("c1"); r.Base != "dev" || r.MergeBase != "def" {
		t.Fatalf("a second start did not move the base: %+v", r)
	}
}

func TestACardWithNoReviewHasNone(t *testing.T) {
	s := open(t)
	if _, err := s.ReviewOf("nope"); err != ErrNoReview {
		t.Fatalf("%v", err)
	}
	if _, err := s.FindingGet("nope", 1); err != ErrNoFinding {
		t.Fatalf("%v", err)
	}
	if halted, _ := s.Halted(); halted {
		t.Fatal("asking for a review that is not there halted the store")
	}
}

func TestFindingsAreNumberedPerCardAndTheNextOneWaits(t *testing.T) {
	s := open(t)
	add := func(card string) int {
		n, err := s.FindingAdd(Finding{Card: card, Cat: "DEAD", Grp: "single"})
		if err != nil {
			t.Fatal(err)
		}
		return n
	}
	if a, b, c := add("c1"), add("c1"), add("c2"); a != 1 || b != 2 || c != 1 {
		t.Fatalf("numbers %d %d %d", a, b, c)
	}
	f1, _ := s.FindingGet("c1", 1)
	f2, _ := s.FindingGet("c1", 2)
	if f1.State != FindingWaiting || f2.State != FindingQueued {
		t.Fatalf("%s %s", f1.State, f2.State)
	}
	if err := s.FindingSetState("c1", 1, FindingKept, "no"); err != nil {
		t.Fatal(err)
	}
	f1, _ = s.FindingGet("c1", 1)
	f2, _ = s.FindingGet("c1", 2)
	if f1.State != FindingKept || f1.Reason != "no" || f2.State != FindingWaiting {
		t.Fatalf("%+v %+v", f1, f2)
	}
}
