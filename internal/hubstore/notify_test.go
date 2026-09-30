package hubstore

import (
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// A SEED STORES AND SAYS NOTHING. It is what a room's first announcement ever
// and the moment the feature is turned on both do, so neither floods.
func TestNotifySeedStoresWithoutFiring(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	fire, err := s.NotifyRecord(r.ID, map[string]string{"a": "a|permission|1"}, []string{"a"}, true)
	if err != nil || len(fire) != 0 {
		t.Fatalf("a seed fired %v (%v)", fire, err)
	}
	if n, _ := s.NotifyCount(r.ID); n != 1 {
		t.Fatalf("the seed left %d rows, want 1", n)
	}
	// The same identity afterwards is nothing.
	fire, _ = s.NotifyRecord(r.ID, map[string]string{"a": "a|permission|1"}, []string{"a"}, false)
	if len(fire) != 0 {
		t.Fatalf("a seeded identity fired again: %v", fire)
	}
}

// A CHANGED OR NEW IDENTITY FIRES ONCE, and the same one again does not.
func TestNotifyChangedIdentityFiresOnce(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	ids := map[string]string{"a": "a|question|1"}
	fire, _ := s.NotifyRecord(r.ID, ids, []string{"a"}, false)
	if !reflect.DeepEqual(fire, []string{"a"}) {
		t.Fatalf("a new identity fired %v", fire)
	}
	fire, _ = s.NotifyRecord(r.ID, ids, []string{"a"}, false)
	if len(fire) != 0 {
		t.Fatalf("the same identity fired again: %v", fire)
	}
	fire, _ = s.NotifyRecord(r.ID, map[string]string{"a": "a|question|2"}, []string{"a"}, false)
	if !reflect.DeepEqual(fire, []string{"a"}) {
		t.Fatalf("a second question fired %v", fire)
	}
}

// A CARD THAT LEAVES AND RETURNS UNCHANGED DOES NOT FIRE. The row is kept when
// the card goes, so an offline room or a restarted one is not a second buzz.
func TestNotifyCardLeavingAndReturningDoesNotRefire(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	id := map[string]string{"a": "a|finished|9"}
	_, _ = s.NotifyRecord(r.ID, id, []string{"a"}, false)
	// An announcement without the card, several of them.
	for i := 0; i < 3; i++ {
		_, _ = s.NotifyRecord(r.ID, map[string]string{}, []string{"b"}, false)
	}
	if n, _ := s.NotifyCount(r.ID); n != 1 {
		t.Fatalf("the row went when its card left: %d rows", n)
	}
	fire, _ := s.NotifyRecord(r.ID, id, []string{"a"}, false)
	if len(fire) != 0 {
		t.Fatalf("a returning card fired again: %v", fire)
	}
}

// ONLY A CARD ABSENT FOR MORE THAN SEVEN DAYS IS PRUNED, and a card that keeps
// appearing is not, however old its identity.
func TestNotifyPruneMeasuresAbsence(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	was := now
	defer func() { now = was }()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	_, _ = s.NotifyRecord(r.ID, map[string]string{"gone": "g|input|1", "here": "h|input|1"},
		[]string{"gone", "here"}, false)

	// Six days on: nothing is old enough.
	now = func() time.Time { return base.Add(6 * 24 * time.Hour) }
	_, _ = s.NotifyRecord(r.ID, map[string]string{"here": "h|input|1"}, []string{"here"}, false)
	if n, _ := s.NotifyPrune(); n != 0 {
		t.Fatalf("pruned %d rows at six days", n)
	}
	// Eight days on: the absent one has been gone eight days, the present one was seen just now.
	now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	_, _ = s.NotifyRecord(r.ID, map[string]string{"here": "h|input|1"}, []string{"here"}, false)
	if n, _ := s.NotifyPrune(); n != 1 {
		t.Fatalf("pruned %d rows at eight days, want the one absent card", n)
	}
	if n, _ := s.NotifyCount(r.ID); n != 1 {
		t.Fatalf("%d rows left, want the present card's", n)
	}
}

// THE MIGRATION TOLERATES BEING THERE: opening the same file twice is not an
// error and keeps what was written.
func TestNotifyMigrationIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	r, _ := s.Add("sparta", TransportDirect)
	_, _ = s.NotifyRecord(r.ID, map[string]string{"a": "a|input|1"}, []string{"a"}, true)
	s.Close()
	s, err = Open(path)
	if err != nil {
		t.Fatalf("reopening: %v", err)
	}
	defer s.Close()
	if n, _ := s.NotifyCount(r.ID); n != 1 {
		t.Fatalf("a reopen lost the rows: %d", n)
	}
}
