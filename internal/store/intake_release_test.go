package store

import (
	"strings"
	"testing"
)

// A released key lets the same item be offered again as a new card, and only
// that one card is touched.
func TestReleaseIntakeKeyLetsTheNextOfferInsert(t *testing.T) {
	s := open(t)
	first, created, err := s.Offer(item("runner-update", "@openai/codex"))
	if err != nil || !created {
		t.Fatalf("first offer: created=%v err=%v", created, err)
	}
	if err := s.SetStatus(first.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	again, created, err := s.Offer(item("runner-update", "@openai/codex"))
	if err != nil || created || again.ID != first.ID {
		t.Fatalf("before release: created=%v id=%s err=%v", created, again.ID, err)
	}

	if err := s.ReleaseIntakeKey(first.ID); err != nil {
		t.Fatal(err)
	}
	second, created, err := s.Offer(item("runner-update", "@openai/codex"))
	if err != nil {
		t.Fatal(err)
	}
	if !created || second.ID == first.ID {
		t.Fatalf("after release: created=%v, same card=%v", created, second.ID == first.ID)
	}
	old, err := s.Get(first.ID)
	if err != nil || old.Title != first.Title || old.Source != "runner-update" {
		t.Fatalf("the released card changed: %+v err=%v", old, err)
	}
}

// Withdrawing archives an inbox card, says why, and frees the key. A started
// card is left alone.
func TestWithdrawOfferedOnlyTakesAnInboxCard(t *testing.T) {
	s := open(t)
	inbox, _, _ := s.Offer(item("runner-update", "a"))
	started, _, _ := s.Offer(item("runner-update", "b"))
	if err := s.SetStatus(started.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}

	done, err := s.WithdrawOffered(inbox.ID, "already installed")
	if err != nil || !done {
		t.Fatalf("inbox card: done=%v err=%v", done, err)
	}
	got, _ := s.Get(inbox.ID)
	if got.ArchivedAt == nil || got.IntakeKey != "" {
		t.Fatalf("archived=%v key=%q", got.ArchivedAt, got.IntakeKey)
	}
	evs, _ := s.Events(inbox.ID, 10)
	if last := evs[len(evs)-1]; !strings.Contains(string(last.Payload), "already installed") {
		t.Fatalf("no reason on the event: %s", last.Payload)
	}
	if again, _ := s.WithdrawOffered(inbox.ID, "x"); again {
		t.Fatal("withdrew a card twice")
	}
	if done, _ := s.WithdrawOffered(started.ID, "x"); done {
		t.Fatal("withdrew a started card")
	}
	if _, created, _ := s.Offer(item("runner-update", "a")); !created {
		t.Fatal("a withdrawn card still blocks its key")
	}
}
