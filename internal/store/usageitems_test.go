package store

import (
	"testing"
	"time"
)

// Counted and cache_read per item equal the linked cards' rows, a card shared by
// two items is split evenly, and an accepted item with no card is unlinked.
func TestAcceptedItemUsage(t *testing.T) {
	s := openTestStore(t)
	at := time.Now().UTC().Add(-time.Hour)
	item := func(id, state, continues string, when time.Time) {
		t.Helper()
		if _, err := s.db.Exec(`INSERT INTO work_item (task_id, title, state, state_at, continues, created_at)
			VALUES (?, ?, ?, ?, ?, ?)`, id, "t-"+id, state, ts(when), continues, ts(when)); err != nil {
			t.Fatal(err)
		}
	}
	row := func(card string, in, out, w5, w1, read int64) {
		t.Helper()
		if err := s.AddSessionUsage(&SessionUsage{TaskID: card, Ended: at, Input: in, Output: out,
			CacheWrite5m: w5, CacheWrite1h: w1, CacheRead: read}); err != nil {
			t.Fatal(err)
		}
	}
	// a alone. b continues c, so b's item has two cards. d and e both continue
	// f, so f is shared by two accepted items. g has no rows. h is not accepted.
	item("a", WorkAccepted, "", at)
	item("b", WorkAccepted, "c", at)
	item("c", WorkSuperseded, "", at)
	item("d", WorkAccepted, "f", at)
	item("e", WorkAccepted, "f", at)
	item("f", WorkSuperseded, "", at)
	item("g", WorkAccepted, "", at)
	item("h", WorkOpen, "", at)
	item("old", WorkAccepted, "", at.Add(-48*time.Hour))
	row("a", 1, 2, 3, 4, 100)
	row("a", 1, 0, 0, 0, 50)
	row("b", 10, 0, 0, 0, 1)
	row("c", 20, 0, 0, 0, 2)
	row("d", 5, 0, 0, 0, 0)
	row("e", 7, 0, 0, 0, 0)
	row("f", 8, 0, 0, 0, 10)
	row("h", 999, 0, 0, 0, 999)

	got, err := s.AcceptedItemUsage(at.Add(-time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]*ItemUsage{}
	for _, it := range got.Items {
		by[it.Item] = it
	}
	if len(by) != 4 || got.Unlinked != 1 {
		t.Fatalf("items %v, unlinked %d", by, got.Unlinked)
	}
	if a := by["a"]; a.Counted != 11 || a.CacheRead != 150 || a.Cards != 1 || a.Split != 1 || a.Title != "t-a" {
		t.Fatalf("a %+v", a)
	}
	if b := by["b"]; b.Counted != 30 || b.CacheRead != 3 || b.Cards != 2 {
		t.Fatalf("b %+v", b)
	}
	if d := by["d"]; d.Counted != 5+4 || d.CacheRead != 5 || d.Split != 2 || d.Cards != 2 {
		t.Fatalf("d %+v", d)
	}
	if e := by["e"]; e.Counted != 7+4 || e.Split != 2 {
		t.Fatalf("e %+v", e)
	}
}
