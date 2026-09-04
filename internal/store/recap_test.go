package store

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestARecapRoundTrips(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "r1", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if task.Recapped() {
		t.Fatal("a new card claims to have a recap")
	}

	if err := s.SetRecap(task.ID, "  bumped the dep and opened a pull request.  "); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Recap != "bumped the dep and opened a pull request." {
		t.Fatalf("the recap came back as %q", got.Recap)
	}
	if !got.Recapped() {
		t.Fatal("a card with a recap says it has none")
	}
	if got.RecapAt == nil {
		t.Fatal("nothing recorded when it was written")
	}
}

// Too long is truncated rather than refused. A session that wrote too much
// still wrote something worth keeping, and failing the call would make an
// agent retry, and the retry would be longer.
func TestAnOverlongRecapIsCutRatherThanRefused(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "r2", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	huge := strings.Repeat("a diff line that should not be here. ", 500)
	if err := s.SetRecap(task.ID, huge); err != nil {
		t.Fatalf("an overlong recap was refused: %v", err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Recap) > MaxRecap+4 {
		t.Fatalf("the recap is %d bytes, over the %d limit", len(got.Recap), MaxRecap)
	}
	if !strings.HasSuffix(got.Recap, "...") {
		t.Fatal("a truncated recap does not say it was cut")
	}
}

// Cutting must never produce invalid UTF-8 in a field the board renders.
func TestTruncationDoesNotSplitARune(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "r3", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}

	// Three byte runes, so a naive cut at a byte offset lands mid-rune for two
	// out of every three possible lengths.
	for _, pad := range []int{0, 1, 2} {
		body := strings.Repeat("x", pad) + strings.Repeat("\u4e16", MaxRecap)
		if err := s.SetRecap(task.ID, body); err != nil {
			t.Fatal(err)
		}
		got, err := s.Get(task.ID)
		if err != nil {
			t.Fatal(err)
		}
		if !utf8.ValidString(got.Recap) {
			t.Fatalf("padding %d produced invalid utf-8", pad)
		}
	}
}

// Clearing is the operator deciding an account was wrong. It takes the time
// with it, so nothing claims a card was written up.
func TestClearingARecapClearsItsTime(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "r4", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecap(task.ID, "something"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecap(task.ID, ""); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Recapped() {
		t.Fatal("the recap was not cleared")
	}
	if got.RecapAt != nil {
		t.Fatal("a cleared recap left a time behind, so the card claims it was written up")
	}
}

// Whitespace is not an account of anything.
func TestWhitespaceIsNotARecap(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "r5", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetRecap(task.ID, "   \n\t  "); err != nil {
		t.Fatal(err)
	}
	got, err := s.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Recapped() {
		t.Fatal("whitespace counted as a recap")
	}
}
