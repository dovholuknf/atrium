//go:build integration

package store

import (
	"testing"
	"time"
)

// A reading is written only when the percent or the reset changes, pruned past
// its keep by kind, and served by since.
func TestLimitReadings(t *testing.T) {
	s := openTestStore(t)
	at := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	reset := at.Add(time.Hour)
	add := func(kind string, pct int, resets time.Time, when time.Time) bool {
		t.Helper()
		ok, err := s.AddLimitReading(LimitReading{Card: "c1", Kind: kind, Pct: pct, ResetsAt: resets, At: when})
		if err != nil {
			t.Fatal(err)
		}
		return ok
	}
	if !add(LimitFiveHour, 10, reset, at) {
		t.Fatal("first reading not written")
	}
	if add(LimitFiveHour, 10, reset, at.Add(time.Minute)) {
		t.Fatal("an unchanged reading was written")
	}
	if !add(LimitFiveHour, 11, reset, at.Add(2*time.Minute)) || !add(LimitFiveHour, 11, reset.Add(5*time.Hour), at.Add(3*time.Minute)) {
		t.Fatal("a changed pct or reset was not written")
	}
	if !add(LimitWeekly, 10, reset, at) {
		t.Fatal("weekly is its own kind")
	}
	got, err := s.LimitReadings(at.Add(time.Minute))
	if err != nil || len(got) != 2 || got[0].Pct != 11 || got[0].Card != "c1" {
		t.Fatalf("since read %+v (%v)", got, err)
	}
	// 9 hours on: the five-hour rows go, the weekly one stays.
	if n, err := s.PruneLimitReadings(at.Add(9 * time.Hour)); err != nil || n != 3 {
		t.Fatalf("pruned %d (%v), want 3", n, err)
	}
	if got, _ := s.LimitReadings(at.Add(-time.Hour)); len(got) != 1 || got[0].Kind != LimitWeekly {
		t.Fatalf("after prune %+v", got)
	}
	if n, _ := s.PruneLimitReadings(at.Add(8 * 24 * time.Hour)); n != 1 {
		t.Fatalf("weekly pruned %d, want 1", n)
	}
}
