//go:build integration

package hubstore

import "testing"

func onWork(id, room, card, work string, rev int) Item {
	it := anItem(rev, ItemStateRunning)
	it.ID, it.Room, it.Card, it.WorkID = id, room, card, work
	return it
}

// TWO CARDS ON ONE BACKLOG ID MAKE ONE WORK ROW AND TWO ATTACHMENTS.
func TestTwoCardsOnOneIdMakeOneWorkRowWithTwoCards(t *testing.T) {
	s := open(t)
	for _, it := range []Item{onWork("a1", "sg4", "c1", "f-thing", 1), onWork("a2", "sg5", "c2", "f-thing", 1)} {
		if _, _, err := s.PutItem(it); err != nil {
			t.Fatal(err)
		}
	}
	all, _ := s.WorkList(0)
	if len(all) != 1 || all[0].BacklogID != "f-thing" {
		t.Fatalf("work rows = %+v", all)
	}
	cards, _ := s.WorkCards("f-thing")
	if len(cards) != 2 {
		t.Fatalf("work_card rows = %+v", cards)
	}
	if as, _ := s.WorkAssignments("f-thing"); len(as) != 2 {
		t.Fatalf("assignments = %+v", as)
	}
}

// A REPLAYED PUSH, AND A ROOM'S OUTBOX AFTER A RECONNECT, CHANGE NOTHING.
func TestAReplayedPushChangesNoWork(t *testing.T) {
	s := open(t)
	it := onWork("a1", "sg4", "c1", "f-thing", 1)
	s.PutItem(it)
	if _, applied, err := s.PutItem(it); err != nil || applied {
		t.Fatalf("replay applied=%v err=%v", applied, err)
	}
	if cards, _ := s.WorkCards("f-thing"); len(cards) != 1 {
		t.Fatalf("work_card rows = %+v", cards)
	}
	// the later push of the same assignment, after the room was down, attaches nothing new
	it.Revision, it.State = 2, ItemStateFinished
	s.PutItem(it)
	if cards, _ := s.WorkCards("f-thing"); len(cards) != 1 {
		t.Fatalf("work_card rows after a later push = %+v", cards)
	}
	if all, _ := s.WorkList(0); len(all) != 1 {
		t.Fatalf("work rows = %+v", all)
	}
}

// A PUSH THAT NAMES NO BACKLOG ID IS ADHOC, which is what a room older than this sends.
func TestAPushWithNoWorkIdIsAdhoc(t *testing.T) {
	s := open(t)
	got, _, err := s.PutItem(onWork("a1", "sg4", "c1", "", 1))
	if err != nil {
		t.Fatal(err)
	}
	if got.WorkID != "adhoc-a1" {
		t.Fatalf("work id = %q", got.WorkID)
	}
	if _, err := s.WorkByID("adhoc-a1"); err != nil {
		t.Fatalf("no adhoc work row: %v", err)
	}
}

// THE BACKFILL: an assignment from before this stage has no work id, and the migration gives it an adhoc one. Run twice.
func TestTheWorkBackfillMakesAdhocRowsAndIsIdempotent(t *testing.T) {
	s := open(t)
	s.PutItem(onWork("old1", "sg4", "c1", "f-x", 1))
	// put the database back to how it stood at 0019: no work rows, no work id on the assignment
	for _, q := range []string{`DELETE FROM work_card`, `DELETE FROM work`, `UPDATE item SET work_id = ''`} {
		if _, err := s.db.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
	var stmts []string
	for _, m := range migrations {
		if m.name == "0022_work_backfill" {
			stmts = m.stmts
		}
	}
	for run := 0; run < 2; run++ {
		for _, st := range stmts {
			if _, err := s.db.Exec(st); err != nil {
				t.Fatalf("run %d: %q: %v", run, st, err)
			}
		}
		it, _ := s.ItemByID("old1")
		if it.WorkID != "adhoc-old1" {
			t.Fatalf("run %d: work id = %q", run, it.WorkID)
		}
		if all, _ := s.WorkList(0); len(all) != 1 {
			t.Fatalf("run %d: work rows = %+v", run, all)
		}
		if cards, _ := s.WorkCards("adhoc-old1"); len(cards) != 1 {
			t.Fatalf("run %d: work_card rows = %+v", run, cards)
		}
	}
}
