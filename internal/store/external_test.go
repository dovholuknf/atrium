package store

import (
	"testing"
	"time"
)

func TestUpsertExternalCreatesThenUpdates(t *testing.T) {
	s := open(t)
	e := External{
		ID: "sess-1", ResumeID: "claude-abc", Title: "fix the tunnel",
		Repo: "ziti", Worktree: "d:/worktrees/ziti/fix", Branch: "fix",
		WindowName: "ziti", Runner: "claude", PID: 4242,
		Status: StatusRunning, LastActivity: time.Now().Add(-time.Minute),
	}
	first, created, err := s.UpsertExternal(e)
	if err != nil || !created {
		t.Fatalf("first upsert: created=%v err=%v", created, err)
	}
	if first.ResumeID != "claude-abc" || first.Branch != "fix" || first.WindowName != "ziti" {
		t.Fatalf("ledger fields not stored: %+v", first)
	}

	e.Status = StatusNeedsInput
	e.PID = 4243
	second, created, err := s.UpsertExternal(e)
	if err != nil {
		t.Fatal(err)
	}
	if created {
		t.Fatal("second upsert created a duplicate card")
	}
	if first.ID != second.ID {
		t.Fatalf("card id changed: %s then %s", first.ID, second.ID)
	}
	if second.Status != StatusNeedsInput || second.PID != 4243 {
		t.Fatalf("observed fields did not refresh: %+v", second)
	}
	if second.WaitingSince == nil {
		t.Fatal("entering needs-input did not start the wait clock")
	}
}

// A session that later connects through the hook must land on the card the
// ledger already made, not a second one.
func TestExternalAndAgentConverge(t *testing.T) {
	s := open(t)
	wt := "d:/worktrees/ziti/fix"
	ext, _, err := s.UpsertExternal(External{
		ID: "sess-2", Worktree: wt, Branch: "fix", Runner: "claude",
		PID: 10, Status: StatusRunning,
	})
	if err != nil {
		t.Fatal(err)
	}
	// The ledger card exists. Now the same worktree reports over the wire.
	again, _, err := s.UpsertExternal(External{
		ID: "sess-2", Worktree: wt, Branch: "fix", Runner: "claude",
		PID: 11, Status: StatusNeedsInput,
	})
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != ext.ID {
		t.Fatal("same session produced two cards")
	}
}

// Putting a card down has to stick. The next ledger poll saying "idle" must
// not drag it back onto the board.
func TestShelvedCardIgnoresLedgerStatus(t *testing.T) {
	s := open(t)
	e := External{ID: "sess-3", Worktree: "d:/w", Runner: "claude", PID: 5, Status: StatusRunning}
	task, _, err := s.UpsertExternal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetStatus(task.ID, StatusShelved); err != nil {
		t.Fatal(err)
	}
	e.Status = StatusNeedsInput
	after, _, err := s.UpsertExternal(e)
	if err != nil {
		t.Fatal(err)
	}
	if after.Status != StatusShelved {
		t.Fatalf("ledger overrode a shelved card, status is %q", after.Status)
	}
}

// Overrides outrank anything the ledger reports.
func TestExternalNeverClobbersOverrides(t *testing.T) {
	s := open(t)
	e := External{ID: "sess-4", Worktree: "d:/w", Branch: "main", Runner: "claude",
		PID: 6, Status: StatusRunning}
	task, _, err := s.UpsertExternal(e)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetOverrides(task.ID, map[string]string{"title": "the auth bug"}); err != nil {
		t.Fatal(err)
	}
	after, _, err := s.UpsertExternal(e)
	if err != nil {
		t.Fatal(err)
	}
	if after.DisplayTitle() != "the auth bug" {
		t.Fatalf("ledger clobbered the override: %q", after.DisplayTitle())
	}
}

func TestExternalIDsListsAdopted(t *testing.T) {
	s := open(t)
	for _, id := range []string{"a", "b"} {
		if _, _, err := s.UpsertExternal(External{
			ID: id, Worktree: "d:/" + id, Runner: "claude", PID: 1, Status: StatusRunning,
		}); err != nil {
			t.Fatal(err)
		}
	}
	// A card that came from an agent, not the ledger, must not appear.
	if _, _, err := s.Register(Observed{WireName: "wire", Worktree: "d:/wire", Runner: "claude"}); err != nil {
		t.Fatal(err)
	}
	ids, err := s.ExternalIDs()
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids["a"] == "" || ids["b"] == "" {
		t.Fatalf("unexpected external ids: %+v", ids)
	}
}
