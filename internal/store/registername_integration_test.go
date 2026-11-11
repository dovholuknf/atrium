//go:build integration

package store

import "testing"

// r-040 setup: a done card holding X, and a live card under Y with pid P and
// worktree W.
func nameCollisionSetup(t *testing.T) (s *Store, done, live *Task) {
	t.Helper()
	s = openTestStore(t)
	done = claimCard(t, s, "x-name")
	if err := s.SetStatus(done.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	live, _, err := s.Register(Observed{WireName: "y-name", Worktree: "/tmp/w", Runner: "claude", PID: 4242})
	if err != nil {
		t.Fatal(err)
	}
	return s, done, live
}

func mustNotBeHalted(t *testing.T, s *Store) {
	t.Helper()
	if halted, cause := s.Halted(); halted {
		t.Fatalf("the store halted: %v", cause)
	}
}

// r-040: a directory-derived name that only a done card holds, arriving with the
// pid and worktree of a DIFFERENT live card, must not rename that card onto it.
func TestRegisterDirNameNeverRenamesALiveCardOntoADoneOne(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	s, done, live := nameCollisionSetup(t)
	got, created, err := s.Register(Observed{WireName: "x-name", NameSource: NameFromDir,
		Worktree: "/tmp/w", Runner: "claude", PID: 4242})
	if err != nil || created || got.ID != live.ID {
		t.Fatalf("want the live card back: %v %v %v", got, created, err)
	}
	mustNotBeHalted(t, s)
	l, _ := s.Get(live.ID)
	if l.WireName != "y-name" || l.PID != 4242 || l.Worktree != "/tmp/w" {
		t.Fatalf("live card changed: %+v", l)
	}
	if d, _ := s.Get(done.ID); d.WireName != "x-name" {
		t.Fatalf("done card lost its name: %+v", d)
	}
}

// A told name still matches the done card, as before.
func TestRegisterToldNameStillMatchesTheDoneCard(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: a real sqlite database on disk")
	}
	t.Parallel()
	s, done, _ := nameCollisionSetup(t)
	got, created, err := s.Register(Observed{WireName: "x-name", Worktree: "/tmp/w", PID: 4242})
	if err != nil || created || got.ID != done.ID {
		t.Fatalf("a told name must match the done card: %v %v %v", got, created, err)
	}
	mustNotBeHalted(t, s)
}

// The guard in refreshObserved: a rename onto a held name keeps the old name.
func TestRefreshObservedKeepsNameWhenAnotherRowHoldsIt(t *testing.T) {
	t.Parallel()
	s, _, live := nameCollisionSetup(t)
	err := s.guard(func() error {
		return s.refreshObserved(live, Observed{WireName: "x-name", Worktree: "/tmp/w", PID: 4243})
	})
	if err != nil {
		t.Fatal(err)
	}
	mustNotBeHalted(t, s)
	l, _ := s.Get(live.ID)
	if l.WireName != "y-name" || l.PID != 4243 {
		t.Fatalf("want old name and new pid: %+v", l)
	}
	if live.WireName != "y-name" {
		t.Fatalf("in-memory card renamed: %s", live.WireName)
	}
}

// A hook's report carries no repo, and must not blank the one the launch recorded: the board colours a card by it.
func TestRegisterWithNoRepoKeepsTheCardsRepo(t *testing.T) {
	t.Parallel()
	s := openTestStore(t)
	card, _, err := s.Register(Observed{WireName: "csharp-ci", Worktree: "/cards/csharp-ci/work", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetPlace(card.ID, Place{Repo: "D:/git/github/openziti/ziti-sdk-csharp", Host: "github", Org: "openziti"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(Observed{WireName: "csharp-ci", Runner: "claude", PID: 77}); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Get(card.ID); got.Repo != "D:/git/github/openziti/ziti-sdk-csharp" {
		t.Fatalf("repo = %q after a report with none", got.Repo)
	}
}
