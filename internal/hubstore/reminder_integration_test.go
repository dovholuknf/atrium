//go:build integration

package hubstore

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func remindAt(card string, due time.Time) Reminder {
	return Reminder{Card: card, Text: "delete the old folder", DueAt: due, CreatedBy: "me@alpha"}
}

// A REMINDER IS KEPT ON THE HUB: it is still pending, with its text and time, after the store is closed and opened again.
func TestAReminderSurvivesTheStoreBeingReopened(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hub.db")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.SetClock(func() time.Time { return now })
	if _, err := s.ReminderAdd(remindAt("alpha~a", now.Add(72*time.Hour))); err != nil {
		t.Fatal(err)
	}
	s.Close()

	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	s.SetClock(func() time.Time { return now })
	got, err := s.ReminderPendingOf("alpha~a")
	if err != nil || len(got) != 1 || got[0].Text != "delete the old folder" || !got[0].DueAt.Equal(now.Add(72*time.Hour)) {
		t.Fatalf("after a reopen: %+v (%v)", got, err)
	}
	if due, _ := s.ReminderDue(); len(due) != 0 {
		t.Fatalf("due before its time: %+v", due)
	}
	now = now.Add(72*time.Hour + time.Second)
	if due, _ := s.ReminderDue(); len(due) != 1 {
		t.Fatalf("not due after its time: %+v", due)
	}
}

func TestACancelledOrFiredReminderIsNotPendingOrDue(t *testing.T) {
	s := open(t)
	now := time.Now().UTC().Truncate(time.Millisecond)
	s.SetClock(func() time.Time { return now })
	a, _ := s.ReminderAdd(remindAt("alpha~a", now.Add(time.Hour)))
	b, _ := s.ReminderAdd(remindAt("alpha~a", now.Add(time.Hour)))
	c, _ := s.ReminderAdd(remindAt("alpha~b", now.Add(time.Hour)))
	if ok, err := s.ReminderCancel(a.ID); err != nil || !ok {
		t.Fatalf("cancel: %v %v", ok, err)
	}
	if ok, _ := s.ReminderCancel(a.ID); ok {
		t.Fatal("a cancelled reminder cancelled twice")
	}
	now = now.Add(2 * time.Hour)
	due, _ := s.ReminderDue()
	if len(due) != 2 {
		t.Fatalf("due %+v, want b and c", due)
	}
	if err := s.ReminderFire(b.ID); err != nil {
		t.Fatal(err)
	}
	if n, _ := s.ReminderCancelCard("alpha~b"); n != 1 {
		t.Fatalf("cancelled %d on b's card, want 1", n)
	}
	if err := s.ReminderFire(c.ID); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := s.ReminderGet(c.ID); got.State != ReminderCancelled {
		t.Fatalf("a cancelled reminder fired as %q", got.State)
	}
	if due, _ := s.ReminderDue(); len(due) != 0 {
		t.Fatalf("still due: %+v", due)
	}
}

func TestACardHoldsAtMostFiftyPendingReminders(t *testing.T) {
	s := open(t)
	due := time.Now().Add(time.Hour)
	for i := 0; i < ReminderMaxPending; i++ {
		if _, err := s.ReminderAdd(remindAt("alpha~a", due)); err != nil {
			t.Fatalf("reminder %d: %v", i, err)
		}
	}
	if _, err := s.ReminderAdd(remindAt("alpha~a", due)); !errors.Is(err, ErrReminderFull) {
		t.Fatalf("the 51st: %v", err)
	}
	if _, err := s.ReminderAdd(remindAt("alpha~b", due)); err != nil {
		t.Fatalf("another card: %v", err)
	}
}

// THE BELL ROW IS A GROWLER ON THE CARD: it keeps the card, takes the reminder reason, is raised once per id, and a human's snooze works on it.
func TestAReminderGrowlerKeepsItsCardAndCanBeSnoozed(t *testing.T) {
	s := open(t)
	r := added(t, s, "alpha")
	g := Growl{ID: "remind|1", CardID: "a", Reason: GrowlReminder, Title: "Reminder: x", Body: "x"}
	if raised, err := s.GrowlRaise(r.ID, g); err != nil || !raised {
		t.Fatalf("raise: %v %v", raised, err)
	}
	if raised, _ := s.GrowlRaise(r.ID, g); raised {
		t.Fatal("the same id raised twice")
	}
	got, err := s.GrowlGet("remind|1")
	if err != nil || got.Reason != GrowlReminder || got.CardID != "a" || got.State != GrowlOpen {
		t.Fatalf("row %+v (%v)", got, err)
	}
	// a card sync for the card reasons must not end it
	if _, _, err := s.GrowlSync(r.ID, []string{GrowlPermission, GrowlQuestion}, nil, map[string]bool{"a": true}); err != nil {
		t.Fatal(err)
	}
	snoozed, err := s.GrowlAct("remind|1", GrowlSnoozed, time.Now().Add(time.Hour), "board", "")
	if err != nil || snoozed.State != GrowlSnoozed {
		t.Fatalf("snooze: %+v (%v)", snoozed, err)
	}
}
