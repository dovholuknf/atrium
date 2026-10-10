//go:build integration

package store

import "testing"

// Drafted answers ride on the card, read back as written, and clear with an empty string.
func TestAnswerDraftsRoundTripOnTheCard(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "drafts-1")
	const d = `[{"at":"2026-10-07T10:12:00Z","qs":["a?"],"a":["yes"]}]`
	if err := s.SetAnswerDrafts(c.ID, d); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(c.ID)
	if got.AnswerDrafts != d {
		t.Fatalf("read back %q, want %q", got.AnswerDrafts, d)
	}
	if err := s.SetAnswerDrafts(c.ID, ""); err != nil {
		t.Fatal(err)
	}
	if got, _ = s.Get(c.ID); got.AnswerDrafts != "" {
		t.Fatalf("not cleared: %q", got.AnswerDrafts)
	}
}

// Anything that is not a JSON array is refused, so a bad write cannot break the board's read of the card.
func TestAnswerDraftsRefuseJunk(t *testing.T) {
	s := openTestStore(t)
	c := aliasCard(t, s, "drafts-2")
	if err := s.SetAnswerDrafts(c.ID, "not json"); err == nil {
		t.Fatal("junk was accepted")
	}
}
