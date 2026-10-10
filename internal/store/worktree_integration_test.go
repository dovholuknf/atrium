//go:build integration

package store

import "testing"

func TestObserveAndRegisterNeverMoveASetWorktree(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "w", Worktree: "/launch", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.Register(Observed{WireName: "w", Worktree: "/launch/src", Runner: "claude"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observe(task.ID, Observed{Worktree: "/launch/other", Runner: "claude"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if got.Worktree != "/launch" {
		t.Fatalf("worktree = %q, want /launch", got.Worktree)
	}
}

func TestAnEmptyWorktreeIsFilledOnceThenNeverMoves(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observe(task.ID, Observed{Worktree: "/first", Runner: "claude"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Observe(task.ID, Observed{Worktree: "/second", Runner: "claude"}); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if got.Worktree != "/first" {
		t.Fatalf("worktree = %q, want /first", got.Worktree)
	}
}

func TestADeliberateMoveStillMovesTheWorktree(t *testing.T) {
	s := open(t)
	task, _, err := s.Register(Observed{WireName: "w", Worktree: "/a", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetWorktree(task.ID, "/b"); err != nil {
		t.Fatal(err)
	}
	got, _ := s.Get(task.ID)
	if got.Worktree != "/b" {
		t.Fatalf("worktree = %q, want /b", got.Worktree)
	}
}
