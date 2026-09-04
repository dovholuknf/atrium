package store

import "testing"

// Everything that has ever run here.
//
// The board answers "what wants my attention now" and excludes archived cards
// on purpose. This answers "what have I had running", which is why it must
// include them: a card that was swept is exactly what somebody is looking for.

func ranAndFinished(t *testing.T, s *Store, name, recap string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: name, Worktree: "d:/w/" + name, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	if recap != "" {
		if err := s.SetRecap(task.ID, recap); err != nil {
			t.Fatal(err)
		}
	}
	return task
}

func TestEverRunIncludesArchivedCards(t *testing.T) {
	s := open(t)
	live := ranAndFinished(t, s, "still-here", "")
	swept := ranAndFinished(t, s, "swept-away", "")
	if _, err := s.Archive(0, StatusDone); err != nil {
		t.Fatal(err)
	}
	// Both were done, so both were archived. Bring one back so there is a card
	// on the board too.
	if err := s.SetStatus(live.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}

	got, total, err := s.EverRun(HistoryQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if total != 2 {
		t.Fatalf("everything that has ever run here counted %d", total)
	}
	seen := map[string]bool{}
	for _, x := range got {
		seen[x.ID] = true
	}
	if !seen[swept.ID] {
		t.Fatal("an archived card is invisible to the view that exists to show it")
	}
	if !seen[live.ID] {
		t.Fatal("a card on the board is missing from the history")
	}
}

// The useful cut. A session that finished with a recap said what it did; one
// that did not is either still worth writing up or was never worth starting.
func TestEverRunCutsByWhetherThereIsARecap(t *testing.T) {
	s := open(t)
	written := ranAndFinished(t, s, "written-up", "bumped the dep and opened a pull request")
	silent := ranAndFinished(t, s, "said-nothing", "")

	with, n, err := s.EverRun(HistoryQuery{Recap: RecapWith})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(with) != 1 || with[0].ID != written.ID {
		t.Fatalf("the written-up cut returned %d: %v", n, with)
	}

	without, n, err := s.EverRun(HistoryQuery{Recap: RecapWithout})
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 || len(without) != 1 || without[0].ID != silent.ID {
		t.Fatalf("the never-written-up cut returned %d: %v", n, without)
	}
}

// Whitespace is not an account of anything, and the cut has to agree with
// Recapped() about that or the two halves would not add up to the whole.
func TestAWhitespaceRecapCountsAsNone(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "ws", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	// Written past SetRecap, which trims, so the column really does hold
	// whitespace. This is the state a direct database edit would leave.
	if err := s.guard(func() error {
		_, err := s.db.Exec(`UPDATE task SET recap = '   ' WHERE id = ?`, task.ID)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	_, with, err := s.EverRun(HistoryQuery{Recap: RecapWith})
	if err != nil {
		t.Fatal(err)
	}
	if with != 0 {
		t.Fatal("whitespace counted as having been written up")
	}
	_, without, err := s.EverRun(HistoryQuery{Recap: RecapWithout})
	if err != nil {
		t.Fatal(err)
	}
	if without != 1 {
		t.Fatal("a whitespace recap fell out of both halves")
	}
}

// One box, because a person looking for "that thing about DNS" does not know
// which field they are remembering it from.
func TestSearchLooksInEveryFieldWorthRemembering(t *testing.T) {
	s := open(t)
	task := ranAndFinished(t, s, "searchable", "fixed the dns resolver on resume")
	if err := s.SetWhy(task.ID, "reported by a customer"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTags(task.ID, []string{"tunneler"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetOrigin(task.ID, "zendesk", "12345", ""); err != nil {
		t.Fatal(err)
	}
	ranAndFinished(t, s, "unrelated", "something else entirely")

	for _, term := range []string{"dns", "customer", "tunneler", "12345", "searchable", "DNS"} {
		got, n, err := s.EverRun(HistoryQuery{Search: term})
		if err != nil {
			t.Fatal(err)
		}
		if n != 1 || len(got) != 1 || got[0].ID != task.ID {
			t.Fatalf("searching %q found %d", term, n)
		}
	}
}

// Paged from the start, because this grows forever until somebody turns
// pruning on.
func TestEverRunPages(t *testing.T) {
	s := open(t)
	for i := 0; i < 5; i++ {
		ranAndFinished(t, s, string(rune('a'+i))+"-card", "")
	}

	first, total, err := s.EverRun(HistoryQuery{Limit: 2})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 {
		t.Fatalf("the total is %d", total)
	}
	if len(first) != 2 {
		t.Fatalf("a page of two returned %d", len(first))
	}

	second, _, err := s.EverRun(HistoryQuery{Limit: 2, Offset: 2})
	if err != nil {
		t.Fatal(err)
	}
	if len(second) != 2 {
		t.Fatalf("the second page returned %d", len(second))
	}
	if first[0].ID == second[0].ID {
		t.Fatal("paging returned the same rows twice")
	}

	// The total is the total, not the page.
	last, total, err := s.EverRun(HistoryQuery{Limit: 2, Offset: 4})
	if err != nil {
		t.Fatal(err)
	}
	if total != 5 || len(last) != 1 {
		t.Fatalf("the last page returned %d of %d", len(last), total)
	}
}

// An absurd limit is bounded rather than honored. A view is not a way to ask
// for the whole table.
func TestAnAbsurdLimitIsBounded(t *testing.T) {
	s := open(t)
	for i := 0; i < 3; i++ {
		ranAndFinished(t, s, string(rune('a'+i))+"-x", "")
	}
	got, _, err := s.EverRun(HistoryQuery{Limit: 100000})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("returned %d", len(got))
	}
}
