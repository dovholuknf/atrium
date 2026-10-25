//go:build integration

package store

import (
	"path/filepath"
	"testing"
)

func newItem(id string) ItemRow {
	return ItemRow{ID: id, TaskID: "t1", Generation: 1, OwnerKind: ItemOwnerSession, Owner: "orch", CreatedBy: "orch"}
}

// A NEW ITEM IS RUNNING AT REVISION 1 AND OWES THE HUB ONE PUSH, keyed (item_id, revision, state).
func TestItemCreateMirrorsAndOwesTheHubOnePush(t *testing.T) {
	s := open(t)
	it, err := s.ItemCreate(newItem("it-1"))
	if err != nil || it.State != ItemRunning || it.Revision != 1 {
		t.Fatalf("create: %+v %v", it, err)
	}
	owed, _ := s.ItemsOwed(10)
	if len(owed) != 1 || owed[0].ID != "it-1" || owed[0].Revision != 1 || owed[0].State != ItemRunning {
		t.Fatalf("owed: %+v", owed)
	}
	// creating the same id again is the same item, not a second push
	if _, err := s.ItemCreate(newItem("it-1")); err != nil {
		t.Fatal(err)
	}
	if owed, _ := s.ItemsOwed(10); len(owed) != 1 {
		t.Fatalf("a second create owes %d pushes", len(owed))
	}
}

// EVERY CHANGE IS A NEW REVISION, and a change that changes nothing is not one.
func TestItemTransitionBumpsTheRevisionOnlyOnAChange(t *testing.T) {
	s := open(t)
	s.ItemCreate(newItem("it-1"))
	it, changed, err := s.ItemTransition("it-1", ItemBlocked, "need a key")
	if err != nil || !changed || it.Revision != 2 || it.State != ItemBlocked || it.Note != "need a key" {
		t.Fatalf("blocked: %+v %v %v", it, changed, err)
	}
	again, changed, err := s.ItemTransition("it-1", ItemBlocked, "need a key")
	if err != nil || changed || again.Revision != 2 {
		t.Fatalf("same again: %+v %v %v", again, changed, err)
	}
	it, changed, _ = s.ItemTransition("it-1", ItemRunning, "")
	if !changed || it.Revision != 3 {
		t.Fatalf("running: %+v", it)
	}
	if owed, _ := s.ItemsOwed(10); len(owed) != 3 {
		t.Fatalf("owed %d pushes, want 3 (one per revision)", len(owed))
	}
	if _, _, err := s.ItemTransition("it-9", ItemRunning, ""); err != ErrNoItem {
		t.Fatalf("missing: %v", err)
	}
	if _, _, err := s.ItemTransition("it-1", "delivered", ""); err == nil {
		t.Fatal("a state that is not one was taken")
	}
}

// THE OUTBOX HOLDS WHILE THE HUB IS DOWN, across a restart, and a row leaves only when the hub took it.
func TestItemOutboxSurvivesARestartAndDrainsRowByRow(t *testing.T) {
	path := filepath.Join(t.TempDir(), "atrium.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.ItemCreate(newItem("it-1"))
	s.ItemTransition("it-1", ItemBlocked, "stuck")
	if err := s.ItemPushFailed("it-1", 1, ItemRunning, "the hub is not answering"); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	owed, _ := s.ItemsOwed(10)
	if len(owed) != 2 || owed[0].Revision != 1 || owed[1].Revision != 2 || owed[0].Attempts != 1 {
		t.Fatalf("after a restart: %+v", owed)
	}
	if err := s.ItemPushed("it-1", 1, ItemRunning); err != nil {
		t.Fatal(err)
	}
	owed, _ = s.ItemsOwed(10)
	if len(owed) != 1 || owed[0].Revision != 2 {
		t.Fatalf("after one push: %+v", owed)
	}
	// a replayed ack is not an error
	if err := s.ItemPushed("it-1", 1, ItemRunning); err != nil {
		t.Fatal(err)
	}
}

// THE UNBLOCK IS WAITING UNTIL THE WORKER'S TURN STARTS FROM IT: delivery alone marks it seen, and the state is not
// touched by either.
func TestItemUnblockIsAwaitedNotApplied(t *testing.T) {
	s := open(t)
	s.ItemCreate(newItem("it-1"))
	s.ItemTransition("it-1", ItemBlocked, "stuck")
	if err := s.ItemAwaitUnblock("it-1", "msg-1", "the answer"); err != nil {
		t.Fatal(err)
	}
	it, _ := s.ItemGet("it-1")
	if it.State != ItemBlocked || it.UnblockMsg != "msg-1" || it.UnblockDelivered {
		t.Fatalf("awaiting: %+v", it)
	}
	if id, err := s.ItemUnblockDelivered("other-msg"); err != nil || id != "" {
		t.Fatalf("another message: %q %v", id, err)
	}
	if id, err := s.ItemUnblockDelivered("msg-1"); err != nil || id != "it-1" {
		t.Fatalf("the unblock: %q %v", id, err)
	}
	it, _ = s.ItemGet("it-1")
	if it.State != ItemBlocked || !it.UnblockDelivered {
		t.Fatalf("delivered but not running: %+v", it)
	}
	// running clears the wait
	it, _, _ = s.ItemTransition("it-1", ItemRunning, "")
	if it.UnblockMsg != "" || it.UnblockDelivered {
		t.Fatalf("running kept the wait: %+v", it)
	}
}

func TestItemOfTaskIsTheLatest(t *testing.T) {
	s := open(t)
	if got, _ := s.ItemOfTask("t1"); got != nil {
		t.Fatalf("an item from nowhere: %+v", got)
	}
	s.ItemCreate(newItem("it-1"))
	got, err := s.ItemOfTask("t1")
	if err != nil || got == nil || got.ID != "it-1" {
		t.Fatalf("of task: %+v %v", got, err)
	}
}

func TestItemStuckMigrationIsLast(t *testing.T) {
	if last := migrations[len(migrations)-1].name; last != "0114_codex_no_alt_screen" {
		t.Fatalf("the last migration is %q", last)
	}
}

// THE STUCK MARKS ARE ONCE PER REVISION, AND A NEW REVISION CLEARS THEM.
func TestItemStuckMarksAreOncePerRevision(t *testing.T) {
	s := open(t)
	it, _ := s.ItemCreate(newItem("it-1"))
	if first, _ := s.ItemStuck(it.ID, it.Revision, "owner-silent"); first {
		t.Fatal("a running item was alerted for")
	}
	blocked, _, _ := s.ItemTransition(it.ID, ItemBlocked, "")
	if first, _ := s.ItemStuck(it.ID, blocked.Revision, "owner-silent"); !first {
		t.Fatal("the first alert was not recorded")
	}
	if first, _ := s.ItemStuck(it.ID, blocked.Revision, "owner-gone"); first {
		t.Fatal("alerted twice for one stall")
	}
	if first, _ := s.ItemMissed(it.ID, blocked.Revision, true); !first {
		t.Fatal("the first hand back was not recorded")
	}
	if first, _ := s.ItemMissed(it.ID, blocked.Revision, true); first {
		t.Fatal("handed back twice")
	}
	if first, _ := s.ItemMissed(it.ID, blocked.Revision, false); !first {
		t.Fatal("the owner side is its own")
	}
	if left, _ := s.ItemsBlocked(); len(left) != 0 {
		t.Fatalf("an alerted item is still read: %d", len(left))
	}
	again, _, _ := s.ItemTransition(it.ID, ItemRunning, "")
	blockedAgain, _, _ := s.ItemTransition(again.ID, ItemBlocked, "")
	if blockedAgain.StuckAlerted != "" || blockedAgain.MissedWorker {
		t.Fatalf("a new stall kept the old marks: %+v", blockedAgain)
	}
}
