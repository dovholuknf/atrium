//go:build integration

package store

import (
	"testing"
	"time"
)

func TestHeldNoticeStatsCountUnreadHeldNoticesOnly(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	card, _, err := s.Register(Observed{WireName: "orch", Worktree: "D:/w/orch", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	if n, at, err := s.HeldNoticeStats(card.ID); err != nil || n != 0 || !at.IsZero() {
		t.Fatalf("a fresh card: %d %v %v", n, at, err)
	}
	// Not held: a notice that was typed is not counted.
	if err := s.AppendEvent(card.ID, EventNotified, map[string]any{"by": "auto-context", "done": true}); err != nil {
		t.Fatal(err)
	}
	if err := s.AppendEvent(card.ID, EventNotified, HeldNoticePayload("fyi", "w", "w1", "one")); err != nil {
		t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	if err := s.AppendEvent(card.ID, EventNotified, HeldNoticePayload("silent-stop", "w", "w1", "two")); err != nil {
		t.Fatal(err)
	}
	n, oldest, err := s.HeldNoticeStats(card.ID)
	if err != nil || n != 2 || oldest.IsZero() {
		t.Fatalf("two held: %d %v %v", n, oldest, err)
	}
	// The list the board opens is the same rows the count counts: who, what, oldest first.
	list, err := s.UnreadHeldNotices(card.ID)
	if err != nil || len(list) != 2 || list[0].Text != "one" || list[0].About != "w" || list[0].Source != "fyi" ||
		list[1].Text != "two" || list[0].At == "" {
		t.Fatalf("the unread list: %+v %v", list, err)
	}

	time.Sleep(5 * time.Millisecond)
	through := ts(s.now())
	changed, err := s.MarkNoticesRead(card.ID, through)
	if err != nil || !changed {
		t.Fatalf("read: %v %v", changed, err)
	}
	if n, at, _ := s.HeldNoticeStats(card.ID); n != 0 || !at.IsZero() {
		t.Fatalf("after read: %d %v", n, at)
	}
	if list, _ := s.UnreadHeldNotices(card.ID); len(list) != 0 {
		t.Fatalf("a read card still lists %d", len(list))
	}
	if changed, _ := s.MarkNoticesRead(card.ID, through); changed {
		t.Fatal("reading with nothing unread reported a change")
	}

	time.Sleep(5 * time.Millisecond)
	if err := s.AppendEvent(card.ID, EventNotified, HeldNoticePayload("fyi", "w", "w1", "three")); err != nil {
		t.Fatal(err)
	}
	n, oldest2, _ := s.HeldNoticeStats(card.ID)
	if n != 1 || !oldest2.After(oldest) {
		t.Fatalf("after a new one: %d, oldest %v, want 1 and newer than %v", n, oldest2, oldest)
	}
}
