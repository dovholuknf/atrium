package hubstore

import (
	"errors"
	"testing"
	"time"
)

func growlFor(room, card, reason, since string) Growl {
	return Growl{ID: room + "|" + card + "|" + reason + "|" + since, CardID: card, Reason: reason,
		Title: card + " wants you"}
}

var cardReasons = []string{GrowlPermission, GrowlQuestion}

func liveIDs(t *testing.T, s *Store) map[string]string {
	t.Helper()
	rows, err := s.GrowlLive()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	for _, r := range rows {
		out[r.ID] = r.State
	}
	return out
}

// THE IDENTITY IS THE KEY. The same waiting spell republished is one row, and a
// second spell on the same card replaces the first rather than stacking.
func TestGrowlSameIdentityIsOneRowAndANewSpellReplaces(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	a1 := growlFor("sparta", "a", GrowlQuestion, "1")
	present := map[string]bool{"a": true}
	raised, changed, err := s.GrowlSync(r.ID, cardReasons, []Growl{a1}, present)
	if err != nil || len(raised) != 1 || !changed {
		t.Fatalf("a first growler: raised %v changed %v (%v)", raised, changed, err)
	}
	raised, changed, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a1}, present)
	if len(raised) != 0 || changed {
		t.Fatalf("the same identity again raised %v, changed %v", raised, changed)
	}
	a2 := growlFor("sparta", "a", GrowlQuestion, "2")
	raised, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a2}, present)
	if len(raised) != 1 || raised[0] != a2.ID {
		t.Fatalf("a second spell raised %v", raised)
	}
	live := liveIDs(t, s)
	if len(live) != 1 || live[a2.ID] != GrowlOpen {
		t.Fatalf("after a second spell the live set is %v, want only the new one", live)
	}
	old, _ := s.GrowlGet(a1.ID)
	if old.State != GrowlResolved || old.EndedAt == nil {
		t.Fatalf("the first spell is %q ended %v, want resolved", old.State, old.EndedAt)
	}
}

// A CARD MISSING FROM AN ANNOUNCEMENT IS NOT A REASON ENDED. A room offline, a
// restart or a shelf all take a card out and put it back.
func TestGrowlAbsentCardKeepsItsGrowler(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	a := growlFor("sparta", "a", GrowlPermission, "1")
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, map[string]bool{"a": true})
	_, changed, _ := s.GrowlSync(r.ID, cardReasons, nil, map[string]bool{})
	if changed || liveIDs(t, s)[a.ID] != GrowlOpen {
		t.Fatalf("a card left out of an announcement lost its growler")
	}
	// Present with no reason is the reason ending.
	_, changed, _ = s.GrowlSync(r.ID, cardReasons, nil, map[string]bool{"a": true})
	if !changed || len(liveIDs(t, s)) != 0 {
		t.Fatalf("a card announced with no reason kept its growler")
	}
}

// A SYNC TOUCHES ONLY THE REASONS IT WAS GIVEN, so a card sync cannot end a
// halt, and a filled-in subject survives an announcement that does not know it.
func TestGrowlSyncKeepsOtherReasonsAndFilledSubjects(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	h := Growl{ID: "sparta|halt|1", Reason: GrowlHalt, Title: "sparta has halted"}
	if raised, _, err := s.GrowlRoom(r.ID, GrowlHalt, true, h); err != nil || !raised {
		t.Fatalf("a halt was not raised (%v)", err)
	}
	a := growlFor("sparta", "a", GrowlPermission, "1")
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, map[string]bool{"a": true})
	if ch, err := s.GrowlFill(a.ID, "perm-1", "rm -rf build"); err != nil || !ch {
		t.Fatalf("fill: %v %v", ch, err)
	}
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, map[string]bool{"a": true})
	got, _ := s.GrowlGet(a.ID)
	if got.Subject != "perm-1" || got.Body != "rm -rf build" {
		t.Fatalf("an announcement wiped the filled subject: %+v", got)
	}
	_, _, _ = s.GrowlSync(r.ID, cardReasons, nil, map[string]bool{})
	if liveIDs(t, s)[h.ID] != GrowlOpen {
		t.Fatalf("a card sync ended the room's halt")
	}
}

// ONE HALT PER ROOM AT A TIME. Still halted keeps the id, healthy ends it, and
// halting again is a new growler.
func TestGrowlHaltKeepsItsIDUntilHealthy(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	_, _, _ = s.GrowlRoom(r.ID, GrowlHalt, true, Growl{ID: "sparta|halt|1", Title: "halted", Body: "disk"})
	raised, changed, _ := s.GrowlRoom(r.ID, GrowlHalt, true, Growl{ID: "sparta|halt|2", Title: "halted", Body: "disk"})
	if raised || changed {
		t.Fatalf("a halt still going raised a second growler")
	}
	if live := liveIDs(t, s); len(live) != 1 || live["sparta|halt|1"] == "" {
		t.Fatalf("live %v, want the first halt", live)
	}
	_, changed, _ = s.GrowlRoom(r.ID, GrowlHalt, false, Growl{})
	if !changed || len(liveIDs(t, s)) != 0 {
		t.Fatalf("a healthy room kept its halt")
	}
	raised, _, _ = s.GrowlRoom(r.ID, GrowlHalt, true, Growl{ID: "sparta|halt|3", Title: "halted"})
	if !raised {
		t.Fatalf("halting again did not raise a new growler")
	}
}

// A STALE ACTION ANSWERS THE ROW. A click on an old screen says "already
// handled" rather than nothing, and an unknown id is not found.
func TestGrowlActOnAHandledGrowlerIsStale(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	a := growlFor("sparta", "a", GrowlQuestion, "1")
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, map[string]bool{"a": true})
	got, err := s.GrowlAct(a.ID, GrowlDismissed, time.Time{}, "board", "tab1")
	if err != nil || got.State != GrowlDismissed || got.ChangedVia != "board" || got.ChangedTab != "tab1" {
		t.Fatalf("dismiss: %+v (%v)", got, err)
	}
	got, err = s.GrowlAct(a.ID, GrowlDismissed, time.Time{}, "phone", "")
	if !errors.Is(err, ErrGrowlStale) || got.State != GrowlDismissed || got.ChangedVia != "board" {
		t.Fatalf("a second dismiss: %+v (%v), want stale with the first one's row", got, err)
	}
	if _, err := s.GrowlAct("nope", GrowlDismissed, time.Time{}, "board", ""); !errors.Is(err, ErrGrowlNotFound) {
		t.Fatalf("an unknown id answered %v", err)
	}
	// A dismissed growler stays dismissed while its reason stands.
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, map[string]bool{"a": true})
	if len(liveIDs(t, s)) != 0 {
		t.Fatalf("an announcement brought a dismissed growler back")
	}
}

// UNDISMISS BRINGS THE SAME GROWLER BACK, only while its reason stands. A
// reason that ended while dismissed makes it resolved, and undismiss is stale.
func TestGrowlUndismissOnlyWhileTheReasonStands(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	a := growlFor("sparta", "a", GrowlPermission, "1")
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, map[string]bool{"a": true})
	before, _ := s.GrowlGet(a.ID)
	_ = s.GrowlReminded(a.ID, 2)
	_, _ = s.GrowlAct(a.ID, GrowlDismissed, time.Time{}, "board", "")
	got, err := s.GrowlAct(a.ID, GrowlOpen, time.Time{}, "board", "")
	if err != nil || got.State != GrowlOpen || !got.RaisedAt.Equal(before.RaisedAt) || got.Reminders != 2 {
		t.Fatalf("undismiss: %+v (%v), want open with its raise and reminders kept", got, err)
	}
	if _, err := s.GrowlAct(a.ID, GrowlOpen, time.Time{}, "board", ""); !errors.Is(err, ErrGrowlStale) {
		t.Fatalf("undismissing an open growler answered %v", err)
	}
	_, _ = s.GrowlAct(a.ID, GrowlDismissed, time.Time{}, "board", "")
	_, _, _ = s.GrowlSync(r.ID, cardReasons, nil, map[string]bool{"a": true})
	got, err = s.GrowlAct(a.ID, GrowlOpen, time.Time{}, "board", "")
	if !errors.Is(err, ErrGrowlStale) || got.State != GrowlResolved {
		t.Fatalf("undismiss after the reason ended: %+v (%v), want stale and resolved", got, err)
	}
}

// A SNOOZE COMES BACK AS IF NEW, and does not survive its reason ending.
func TestGrowlSnoozeWakesAndEndsWithItsReason(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	old := now
	base := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	t.Cleanup(func() { now = old })

	a := growlFor("sparta", "a", GrowlQuestion, "1")
	b := growlFor("sparta", "b", GrowlQuestion, "1")
	both := map[string]bool{"a": true, "b": true}
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a, b}, both)
	_ = s.GrowlReminded(a.ID, 3)
	for _, g := range []Growl{a, b} {
		if _, err := s.GrowlAct(g.ID, GrowlSnoozed, base.Add(15*time.Minute), "board", ""); err != nil {
			t.Fatal(err)
		}
	}
	if woke, _ := s.GrowlWake(); len(woke) != 0 {
		t.Fatalf("woke %v before the snooze ended", woke)
	}
	// b's reason ends while it sleeps.
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{a}, both)
	if got, _ := s.GrowlGet(b.ID); got.State != GrowlResolved {
		t.Fatalf("a snoozed growler whose reason ended is %q", got.State)
	}
	now = func() time.Time { return base.Add(16 * time.Minute) }
	woke, _ := s.GrowlWake()
	if len(woke) != 1 || woke[0] != a.ID {
		t.Fatalf("woke %v, want only a", woke)
	}
	got, _ := s.GrowlGet(a.ID)
	if got.State != GrowlOpen || got.Until != nil || got.Reminders != 0 || !got.RaisedAt.Equal(now()) {
		t.Fatalf("a woken growler is %+v, want open, raised now, no reminders", got)
	}
}

// THE PRUNE TAKES WHAT NOBODY WILL LOOK AT AGAIN, and never a dismissal whose
// reason still stands, which would bring it back on the next announcement.
func TestGrowlPrune(t *testing.T) {
	s := open(t)
	r := added(t, s, "sparta")
	old := now
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	now = func() time.Time { return base }
	t.Cleanup(func() { now = old })

	if _, err := s.Announce(r.ID, []Card{{ID: "live", Status: "needs-input", Payload: []byte(`{}`)}}); err != nil {
		t.Fatal(err)
	}
	ended := growlFor("sparta", "ended", GrowlQuestion, "1")
	dismissed := growlFor("sparta", "live", GrowlQuestion, "1")
	vanished := growlFor("sparta", "vanished", GrowlPermission, "1")
	all := map[string]bool{"ended": true, "live": true, "vanished": true}
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{ended, dismissed, vanished}, all)
	_, _ = s.GrowlAct(dismissed.ID, GrowlDismissed, time.Time{}, "board", "")
	_, _, _ = s.GrowlSync(r.ID, cardReasons, []Growl{dismissed, vanished}, all)

	now = func() time.Time { return base.Add(6 * 24 * time.Hour) }
	if n, _ := s.GrowlPrune(); n != 0 {
		t.Fatalf("pruned %d rows at six days", n)
	}
	now = func() time.Time { return base.Add(8 * 24 * time.Hour) }
	if n, _ := s.GrowlPrune(); n != 2 {
		t.Fatalf("pruned %d rows at eight days, want the ended one and the vanished card", n)
	}
	if _, err := s.GrowlGet(dismissed.ID); err != nil {
		t.Fatalf("a dismissal whose card is still cached was pruned: %v", err)
	}
}
