//go:build integration

package store

import (
	"errors"
	"testing"
)

func claimCard(t *testing.T, s *Store, name string) *Task {
	t.Helper()
	task, _, err := s.Register(Observed{WireName: name, Worktree: "/tmp/" + name, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	return task
}

func resumeOf(t *testing.T, s *Store, id string) string {
	t.Helper()
	got, err := s.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.ResumeID
}

// r-021: a claim bound by task id moves a conversation off a card that is not
// live, and is refused against one that is. A claim by name is refused either way.
func TestClaimResumeIDRanksTheClaimant(t *testing.T) {
	s := openTestStore(t)
	old, mine := claimCard(t, s, "old"), claimCard(t, s, "mine")
	if err := s.SetResumeID(old.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	notLive := func(*Task) bool { return false }
	live := func(*Task) bool { return true }

	// By name: refused even though the holder is not live.
	res, err := s.ClaimResumeID(mine.ID, "conv-1", false, notLive)
	if err != nil || res.Stored || res.Refused == nil || res.Refused.ID != old.ID {
		t.Fatalf("a name claim was not refused: %+v, %v", res, err)
	}
	// By id against a live holder: refused.
	res, err = s.ClaimResumeID(mine.ID, "conv-1", true, live)
	if err != nil || res.Stored || res.Refused == nil {
		t.Fatalf("a claim on a live holder was not refused: %+v, %v", res, err)
	}
	if resumeOf(t, s, old.ID) != "conv-1" || resumeOf(t, s, mine.ID) != "" {
		t.Fatal("a refused claim changed a card")
	}
	// By id against a holder with no live session: moved.
	res, err = s.ClaimResumeID(mine.ID, "conv-1", true, notLive)
	if err != nil || !res.Stored || len(res.Moved) != 1 || res.Moved[0].ID != old.ID {
		t.Fatalf("a task-id claim did not move the id: %+v, %v", res, err)
	}
	if resumeOf(t, s, old.ID) != "" || resumeOf(t, s, mine.ID) != "conv-1" {
		t.Fatal("the id did not move")
	}
	// Nobody else holds it: stored by any claimant.
	if res, _ = s.ClaimResumeID(mine.ID, "conv-2", false, nil); !res.Stored {
		t.Fatal("an unheld id was not stored")
	}
}

// r-021: a name taken from the directory never matches a finished or archived
// card. The same name told to the hook still does.
func TestRegisterWillNotMatchADoneCardByADirectoryName(t *testing.T) {
	s := openTestStore(t)
	done := claimCard(t, s, "atriumx")
	if err := s.SetStatus(done.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	_, _, err := s.Register(Observed{WireName: "atriumx", NameSource: NameFromDir})
	if !errors.Is(err, ErrStaleName) {
		t.Fatalf("want ErrStaleName, got %v", err)
	}
	got, created, err := s.Register(Observed{WireName: "atriumx"})
	if err != nil || created || got.ID != done.ID {
		t.Fatalf("a told name must still match: %v %v %v", got, created, err)
	}
	// A live card by that name matches either way.
	if err := s.SetStatus(done.ID, StatusRunning); err != nil {
		t.Fatal(err)
	}
	got, _, err = s.Register(Observed{WireName: "atriumx", NameSource: NameFromDir})
	if err != nil || got.ID != done.ID {
		t.Fatalf("a live card should match a directory name: %v %v", got, err)
	}
}

// r-042: a claimant that does not exist stores nothing and clears nothing.
func TestClaimResumeIDMissingClaimantKeepsTheHolder(t *testing.T) {
	s := openTestStore(t)
	old := claimCard(t, s, "old")
	if err := s.SetResumeID(old.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	res, err := s.ClaimResumeID("no-such-card", "conv-1", true, func(*Task) bool { return false })
	if err != nil || res.Stored || len(res.Moved) != 0 || res.Refused != nil {
		t.Fatalf("a missing claimant: %+v, %v", res, err)
	}
	if resumeOf(t, s, old.ID) != "conv-1" {
		t.Fatal("a missing claimant cleared the holder")
	}
}

// r-042: the archived half of "finished or archived", which the done test skips.
func TestRegisterWillNotMatchAnArchivedCardByADirectoryName(t *testing.T) {
	s := openTestStore(t)
	card := claimCard(t, s, "atriumx")
	if _, err := s.db.Exec(`UPDATE task SET archived_at = ? WHERE id = ?`, ts(now()), card.ID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(Observed{WireName: "atriumx", NameSource: NameFromDir}); !errors.Is(err, ErrStaleName) {
		t.Fatalf("want ErrStaleName, got %v", err)
	}
	got, created, err := s.Register(Observed{WireName: "atriumx"})
	if err != nil || created || got.ID != card.ID {
		t.Fatalf("a told name must still match: %v %v %v", got, created, err)
	}
}

// r-042: a directory name that matches nothing still finds the live card the
// pid and worktree point at, and never a finished one.
func TestRegisterDirectoryNameFallsBackToThePidHint(t *testing.T) {
	s := openTestStore(t)
	card, _, err := s.Register(Observed{WireName: "named", Worktree: "/tmp/w", PID: 4242, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	got, created, err := s.Register(Observed{WireName: "w", NameSource: NameFromDir, Worktree: "/tmp/w", PID: 4242})
	if err != nil || created || got.ID != card.ID {
		t.Fatalf("the pid hint did not match the live card: %v %v %v", got, created, err)
	}
	if err := s.SetStatus(card.ID, StatusDone); err != nil {
		t.Fatal(err)
	}
	got, created, err = s.Register(Observed{WireName: "w2", NameSource: NameFromDir, Worktree: "/tmp/w", PID: 4242})
	if err != nil || !created || got.ID == card.ID {
		t.Fatalf("a finished card matched by pid: %v %v %v", got, created, err)
	}
}

// r-042: SetResumeID ignores a blank, so ClearResumeID is the way to drop a bad id.
func TestClearResumeIDDropsTheId(t *testing.T) {
	s := openTestStore(t)
	c := claimCard(t, s, "c")
	if err := s.SetResumeID(c.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetResumeID(c.ID, ""); err != nil || resumeOf(t, s, c.ID) != "conv-1" {
		t.Fatal("a blank SetResumeID must stay a no-op")
	}
	if err := s.ClearResumeID(c.ID); err != nil || resumeOf(t, s, c.ID) != "" {
		t.Fatalf("the id was not cleared: %v", err)
	}
}
