//go:build integration

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

// notifyAt reads a row's `at` back, which is what the refresh loops write.
func notifyAt(t *testing.T, s *Store, room, card string) string {
	t.Helper()
	var at string
	if err := s.db.QueryRow(`SELECT at FROM notify_sent WHERE room_id = ? AND card_id = ?`, room, card).Scan(&at); err != nil {
		t.Fatalf("reading the row of %s: %v", card, err)
	}
	return at
}

// A CARD PRESENT WITHOUT AN IDENTITY KEEPS ITS ROW AND REFRESHES `at`, but only once the row is older than
// notifyTouch, so a two second announcement is not a write per card. Without the refresh a card that is there but
// quiet would be pruned after seven days like one that left.
func TestNotifyPresentWithoutIdentityRefreshes(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	was := now
	defer func() { now = was }()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	_, _ = s.NotifyRecord(r.ID, map[string]string{"a": "a|input|1"}, []string{"a"}, false)

	// Inside notifyTouch: no write.
	now = func() time.Time { return base.Add(notifyTouch - time.Minute) }
	_, _ = s.NotifyRecord(r.ID, map[string]string{}, []string{"a"}, false)
	if got := notifyAt(t, s, r.ID, "a"); got != ts(base) {
		t.Fatalf("a fresh row was rewritten: at %s, want %s", got, ts(base))
	}

	// Past it: the row's at moves, and the identity stays so the card does not fire when it returns.
	later := base.Add(6 * 24 * time.Hour)
	now = func() time.Time { return later }
	fire, _ := s.NotifyRecord(r.ID, map[string]string{}, []string{"a"}, false)
	if len(fire) != 0 {
		t.Fatalf("a card with no identity fired %v", fire)
	}
	if got := notifyAt(t, s, r.ID, "a"); got != ts(later) {
		t.Fatalf("a stale row was not refreshed: at %s, want %s", got, ts(later))
	}

	// Eight days on from the start, the six day refresh keeps it.
	now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	if n, _ := s.NotifyPrune(); n != 0 {
		t.Fatalf("pruned %d rows of a card that was present at six days", n)
	}
	fire, _ = s.NotifyRecord(r.ID, map[string]string{"a": "a|input|1"}, []string{"a"}, false)
	if len(fire) != 0 {
		t.Fatalf("the kept identity fired again: %v", fire)
	}
}

// AN UNCHANGED IDENTITY IS REFRESHED THE SAME WAY, and a present id with no row is not made one.
func TestNotifyUnchangedIdentityRefreshes(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	was := now
	defer func() { now = was }()
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	ids := map[string]string{"a": "a|input|1"}
	_, _ = s.NotifyRecord(r.ID, ids, []string{"a"}, false)

	later := base.Add(2 * notifyTouch)
	now = func() time.Time { return later }
	fire, _ := s.NotifyRecord(r.ID, ids, []string{"a", "ghost"}, false)
	if len(fire) != 0 {
		t.Fatalf("an unchanged identity fired %v", fire)
	}
	if got := notifyAt(t, s, r.ID, "a"); got != ts(later) {
		t.Fatalf("an unchanged row was not refreshed: at %s, want %s", got, ts(later))
	}
	if n, _ := s.NotifyCount(r.ID); n != 1 {
		t.Fatalf("%d rows, want 1: a present card with no row must not get one", n)
	}
}

// A FAILING STORE IS AN ERROR AND NOT A FIRE. The caller must not notify about what was never recorded, or the
// same card would buzz again on the next announcement.
func TestNotifyErrorReturns(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	_, _ = s.NotifyRecord(r.ID, map[string]string{"a": "a|input|1"}, []string{"a"}, false)
	s.Close()

	fire, err := s.NotifyRecord(r.ID, map[string]string{"b": "b|input|1"}, []string{"b"}, false)
	if err == nil {
		t.Fatal("NotifyRecord on a closed store returned no error")
	}
	if len(fire) != 0 {
		t.Fatalf("NotifyRecord returned %v along with an error", fire)
	}
	if _, err := s.NotifyPrune(); err == nil {
		t.Fatal("NotifyPrune on a closed store returned no error")
	}
	if _, err := s.NotifyCount(r.ID); err == nil {
		t.Fatal("NotifyCount on a closed store returned no error")
	}
}

// A FAILED WRITE RETURNS NO FIRE, with the error.
func TestNotifyFailedWriteFiresNothing(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	if _, err := s.db.Exec(`DROP TABLE notify_sent`); err != nil {
		t.Fatal(err)
	}
	fire, err := s.NotifyRecord(r.ID, map[string]string{"a": "a|input|1"}, []string{"a"}, false)
	if err == nil || len(fire) != 0 {
		t.Fatalf("a missing table gave fire %v and error %v", fire, err)
	}
}

// THE FIRE ORDER IS THE ID ORDER, the same on every call, so a notification burst reads the same each time.
func TestNotifyFireOrderIsStable(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	want := []string{"a", "b", "c", "d", "e", "f", "g", "h"}
	ids := map[string]string{}
	for _, id := range want {
		ids[id] = id + "|input|1"
	}
	fire, err := s.NotifyRecord(r.ID, ids, want, false)
	if err != nil || !reflect.DeepEqual(fire, want) {
		t.Fatalf("fire order %v (%v), want %v", fire, err, want)
	}
	// Changing all of them again gives the same order.
	for id := range ids {
		ids[id] = id + "|input|2"
	}
	fire, _ = s.NotifyRecord(r.ID, ids, nil, false)
	if !reflect.DeepEqual(fire, want) {
		t.Fatalf("second fire order %v, want %v", fire, want)
	}
}
