package store

import (
	"encoding/json"
	"testing"
	"time"
)

// `through` reaches the store from a JSON round trip of an event's `at`, and RFC3339Nano drops
// trailing zeros: ".12Z" and "Z" for a notice held at .120 and at .000. Both must still stamp.
func TestMarkNoticesReadTakesAnAtFromAJSONRoundTrip(t *testing.T) {
	old := now
	t.Cleanup(func() { now = old })

	for _, at := range []string{"2026-01-01T00:00:01.120Z", "2026-01-01T00:00:02.000Z"} {
		s := openTestStore(t)
		card, _, err := s.Register(Observed{WireName: "orch", Worktree: "D:/tmp/orch", Runner: "claude"})
		if err != nil {
			t.Fatal(err)
		}
		when, err := time.Parse(TimeFormat, at)
		if err != nil {
			t.Fatal(err)
		}
		now = func() time.Time { return when }
		if err := s.AppendEvent(card.ID, EventNotified, HeldNoticePayload("fyi", "w", "w1", "one")); err != nil {
			t.Fatal(err)
		}
		now = old

		events, err := s.Events(card.ID, 100)
		if err != nil {
			t.Fatal(err)
		}
		var through string
		for _, e := range events {
			if e.Kind != EventNotified {
				continue
			}
			raw, err := json.Marshal(e)
			if err != nil {
				t.Fatal(err)
			}
			var back struct {
				At string `json:"at"`
			}
			if err := json.Unmarshal(raw, &back); err != nil {
				t.Fatal(err)
			}
			through = back.At
		}
		if through == "" || through == at {
			t.Fatalf("%s: round trip gave %q, want a shortened time", at, through)
		}
		if changed, err := s.MarkNoticesRead(card.ID, through); err != nil || !changed {
			t.Fatalf("%s: stamping %q: %v %v, want the count to fall", at, through, changed, err)
		}
		if n, _, _ := s.HeldNoticeStats(card.ID); n != 0 {
			t.Fatalf("%s: %d unread after stamping %q", at, n, through)
		}
	}
}

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
