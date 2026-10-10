//go:build integration

package store

import (
	"errors"
	"testing"
	"time"
)

func TestAFrozenCardKeepsWhatIsSaidToItAndReplaysItInOrder(t *testing.T) {
	s := openTestStore(t)
	c := mustRegister(t, s, "old")
	if _, err := s.Freeze(c.ID, "m1", time.Hour); err != nil {
		t.Fatal(err)
	}
	a, err := s.QueueFromPeer(c.ID, "one", "x")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := s.QueueMessage(c.ID, "two")
	if p, _ := s.PendingMessages(c.ID); len(p) != 0 {
		t.Fatalf("a frozen card has %d pending messages, want them held in the freeze queue", len(p))
	}
	q, _ := s.FreezeQueue(c.ID)
	if len(q) != 2 || q[0].ID != a.ID || q[1].ID != b.ID || q[0].FromPeer != "x" {
		t.Fatalf("queue = %+v", q)
	}
	n, err := s.Unfreeze(c.ID, "m1", true)
	if err != nil || n != 2 {
		t.Fatalf("replay %d, %v", n, err)
	}
	p, _ := s.PendingMessages(c.ID)
	if len(p) != 2 || p[0].ID != a.ID || p[1].Text != "two" {
		t.Fatalf("after the undo pending = %+v", p)
	}
	if f, _ := s.Frozen(c.ID); f != nil {
		t.Fatal("still frozen")
	}
}

func TestAnAckDropsOneQueuedMessageAndARepeatIsHarmless(t *testing.T) {
	s := openTestStore(t)
	c := mustRegister(t, s, "old")
	s.Freeze(c.ID, "m1", time.Hour)
	a, _ := s.QueueMessage(c.ID, "one")
	s.QueueMessage(c.ID, "two")
	for i := 0; i < 2; i++ {
		if err := s.AckFrozen(c.ID, a.ID); err != nil {
			t.Fatal(err)
		}
	}
	if q, _ := s.FreezeQueue(c.ID); len(q) != 1 || q[0].Text != "two" {
		t.Fatalf("queue = %+v", q)
	}
}

func TestFreezeIsIdempotentByMoveAndRefusesAnotherMove(t *testing.T) {
	s := openTestStore(t)
	c := mustRegister(t, s, "old")
	if _, err := s.Freeze(c.ID, "m1", time.Hour); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Freeze(c.ID, "m1", time.Hour); err != nil {
		t.Fatalf("the same move freezing twice: %v", err)
	}
	if _, err := s.Freeze(c.ID, "m2", time.Hour); !errors.Is(err, ErrFrozenByOther) {
		t.Fatalf("another move froze it: %v", err)
	}
}

// The lease and the cut-over are one decision: whichever commits first refuses the other.
func TestAnExpiredLeaseUndoesTheFreezeAndRefusesMovedTo(t *testing.T) {
	s := openTestStore(t)
	c := mustRegister(t, s, "old")
	s.Freeze(c.ID, "m1", time.Millisecond)
	s.QueueMessage(c.ID, "held")
	time.Sleep(5 * time.Millisecond)
	if err := s.SetMovedTo(c.ID, "m1", "b~new"); !errors.Is(err, ErrLeaseExpired) {
		t.Fatalf("moved_to after the lease: %v", err)
	}
	undone, err := s.ExpireFreeze(c.ID, time.Now())
	if err != nil || !undone {
		t.Fatalf("expire: %v %v", undone, err)
	}
	if p, _ := s.PendingMessages(c.ID); len(p) != 1 {
		t.Fatalf("the queue was not replayed: %+v", p)
	}
}

func TestMovedToRefusesTheSelfUndoAndTheReplay(t *testing.T) {
	s := openTestStore(t)
	c := mustRegister(t, s, "old")
	s.Freeze(c.ID, "m1", time.Hour)
	if err := s.SetMovedTo(c.ID, "m1", "b~new"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMovedTo(c.ID, "m1", "b~new"); err != nil {
		t.Fatalf("a repeat of the same: %v", err)
	}
	if err := s.SetMovedTo(c.ID, "m1", "b~other"); !errors.Is(err, ErrMoved) {
		t.Fatalf("a different target: %v", err)
	}
	s.RenewFreeze(c.ID, "m1", time.Nanosecond)
	time.Sleep(2 * time.Millisecond)
	if undone, _ := s.ExpireFreeze(c.ID, time.Now()); undone {
		t.Fatal("a card that moved undid itself")
	}
	if _, err := s.Unfreeze(c.ID, "m1", true); !errors.Is(err, ErrMoved) {
		t.Fatalf("replay after the move: %v", err)
	}
	if _, err := s.Unfreeze(c.ID, "m1", false); err != nil {
		t.Fatalf("the cut-over's own release: %v", err)
	}
	if got := mustGet(t, s, c.ID); got.MovedTo != "b~new" {
		t.Fatalf("moved_to = %q", got.MovedTo)
	}
}

func TestSeenMoveDropsARepeat(t *testing.T) {
	s := openTestStore(t)
	if f, _ := s.SeenMove("id1", "t"); !f {
		t.Fatal("first is new")
	}
	if f, _ := s.SeenMove("id1", "t"); f {
		t.Fatal("a repeat is new")
	}
}

func TestAMovedCardKeepsItsHandle(t *testing.T) {
	s := openTestStore(t)
	c := mustRegister(t, s, "keeper")
	s.Freeze(c.ID, "m1", time.Hour)
	s.SetMovedTo(c.ID, "m1", "b~new")
	s.Unfreeze(c.ID, "m1", false)
	if !s.MovedHandleHeld(c.WireName) {
		t.Fatal("the handle is not held")
	}
	other, _, err := s.Register(Observed{WireName: "keeper", Worktree: "/tmp/other", Runner: "claude"})
	if err == nil && other.ID == c.ID {
		t.Fatal("a later card took over the moved card")
	}
	if err == nil && other.WireName == c.WireName {
		t.Fatalf("a later card holds the moved card's handle %q", other.WireName)
	}
}
