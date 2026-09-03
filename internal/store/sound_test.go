package store

import "testing"

// A card's own bell. Stored rather than held in the browser, because knowing
// which agent wants you without looking only works if the answer is the same
// tomorrow and in another browser.
func TestSoundIsStoredOnTheCard(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "belled", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Sound != "" {
		t.Fatalf("a new card came with a sound of %q, wanted the board default", task.Sound)
	}

	if err := s.SetSound(task.ID, "knock"); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sound != "knock" {
		t.Fatalf("the card rings with %q, wanted knock", got.Sound)
	}

	// Clearing it is a real choice, not an omission: it means go back to the
	// board's tone.
	if err := s.SetSound(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, err = s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Sound != "" {
		t.Fatalf("clearing left %q behind", got.Sound)
	}
}

// The column was added to a table that already had rows, so a card written
// before the migration has to read back rather than fail the scan.
func TestACardFromBeforeTheSoundColumnStillReads(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "older", Worktree: "/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	// What the row looked like before the column existed. The migration's
	// DEFAULT '' is what makes this readable, and NULL is what it would be
	// without one.
	if _, err := s.db.Exec(`UPDATE task SET sound = '' WHERE id = ?`, task.ID); err != nil {
		t.Fatal(err)
	}
	all, err := s.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) == 0 {
		t.Fatal("no cards came back")
	}
}
