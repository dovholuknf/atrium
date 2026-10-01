package store

import (
	"testing"
	"time"
)

// The marker is the newest notice handed back, never now: a notice held after the read is
// newer than the stamp and stays unread, and a stamp that is old, empty or not a time moves
// nothing.
func TestMarkNoticesReadStopsAtWhatWasRead(t *testing.T) {
	s := openTestStore(t)
	card, _, err := s.Register(Observed{WireName: "orch", Worktree: "D:/tmp/orch", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(card.ID, EventNotified, HeldNoticePayload("fyi", "w", "w1", "one")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	through := ts(now())
	time.Sleep(5 * time.Millisecond)
	if err := s.AppendEvent(card.ID, EventNotified, HeldNoticePayload("fyi", "w", "w1", "two")); err != nil {
		t.Fatal(err)
	}

	for _, bad := range []string{"", "yesterday"} {
		if changed, err := s.MarkNoticesRead(card.ID, bad); err != nil || changed {
			t.Fatalf("stamp %q: %v %v, want no change", bad, changed, err)
		}
	}
	if n, _, _ := s.HeldNoticeStats(card.ID); n != 2 {
		t.Fatalf("%d unread after bad stamps, want 2", n)
	}
	if changed, err := s.MarkNoticesRead(card.ID, through); err != nil || !changed {
		t.Fatalf("stamp: %v %v", changed, err)
	}
	if n, _, _ := s.HeldNoticeStats(card.ID); n != 1 {
		t.Fatalf("%d unread, want the one held after the stamp", n)
	}
	if changed, _ := s.MarkNoticesRead(card.ID, ts(now().Add(-time.Hour))); changed {
		t.Fatal("an older stamp reported a change")
	}
	if n, _, _ := s.HeldNoticeStats(card.ID); n != 1 {
		t.Fatalf("%d unread after an older stamp, want still 1", n)
	}
}
