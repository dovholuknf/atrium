//go:build integration

package hubstore

import (
	"errors"
	"strings"
	"testing"
)

const mqRepo = "github.com/dovholuknf/atrium"

func mqAsk(t *testing.T, s *Store, branch, sha string) MergeRequest {
	t.Helper()
	m, existing, err := s.MergeAsked(MergeAsk{Repo: mqRepo, Branch: branch, SHA: sha, Requester: "w1", RequesterRoom: "SG4"})
	if err != nil || existing {
		t.Fatalf("ask %s: %+v %v %v", branch, m, existing, err)
	}
	return m
}

func TestMergeAskedQueuesInOrderAndAnswersTheOpenRequestAgain(t *testing.T) {
	s := open(t)
	a := mqAsk(t, s, "claude/a", "aaaaaaa1")
	b := mqAsk(t, s, "claude/b", "bbbbbbb1")
	if a.N != 1 || b.N != 2 || a.State != MergeQueued || a.RequesterRoom != "sg4" || a.AskedAt == "" {
		t.Fatalf("rows: %+v %+v", a, b)
	}
	again, existing, err := s.MergeAsked(MergeAsk{Repo: mqRepo, Branch: "claude/a", SHA: "aaaaaaa1"})
	if err != nil || !existing || again.N != 1 {
		t.Fatalf("again: %+v %v %v", again, existing, err)
	}
	if _, _, err := s.MergeAsked(MergeAsk{Repo: mqRepo, Branch: "claude/a", SHA: "ccccccc1"}); !IsRefusal(err) ||
		!strings.Contains(err.Error(), "cancel it first") {
		t.Fatalf("another sha while open: %v", err)
	}
	next, ok, err := s.MergeNext(mqRepo)
	if err != nil || !ok || next.N != 1 {
		t.Fatalf("next: %+v %v %v", next, ok, err)
	}
}

func TestMergeAskedRefusesWhatTheQueueDoesNotMerge(t *testing.T) {
	s := open(t)
	for name, a := range map[string]MergeAsk{
		"claude/main": {Repo: mqRepo, Branch: "claude/main", SHA: "aaaaaaa"},
		"main":        {Repo: mqRepo, Branch: "main", SHA: "aaaaaaa"},
		"repo":        {Repo: "atrium", Branch: "claude/a", SHA: "aaaaaaa"},
		"sha":         {Repo: mqRepo, Branch: "claude/a", SHA: "xyz"},
		"spaces":      {Repo: mqRepo, Branch: "claude/a b", SHA: "aaaaaaa"},
		"continues":   {Repo: mqRepo, Branch: "claude/a", SHA: "aaaaaaa", Continues: 9},
	} {
		if _, _, err := s.MergeAsked(a); !IsRefusal(err) {
			t.Errorf("%s: want a refusal, got %v", name, err)
		}
	}
}

func TestMergeNextRunsOneAtATimeAndAMergersRequestFirst(t *testing.T) {
	s := open(t)
	a := mqAsk(t, s, "claude/a", "aaaaaaa1")
	mqAsk(t, s, "claude/b", "bbbbbbb1")
	if _, err := s.MergeMove(MergeStep{N: a.N, From: MergeQueued, To: MergeMerging, Base: "0000000"}); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := s.MergeNext(mqRepo); err != nil || ok {
		t.Fatalf("a merge is running and next answered one: %v %v", ok, err)
	}
	if repos, _ := s.MergeRepos(); len(repos) != 1 || repos[0] != MergeRepoName(mqRepo) {
		t.Fatalf("repos: %v", repos)
	}
	c, err := s.MergeMove(MergeStep{N: a.N, To: MergeConflict, Reason: "conflict in a.go", Merger: "sg4~m1"})
	if err != nil || c.State != MergeConflict || c.Merger != "sg4~m1" || c.EndedAt != "" {
		t.Fatalf("conflict: %+v %v", c, err)
	}
	fix, _, err := s.MergeAsked(MergeAsk{Repo: mqRepo, Branch: "claude/merge-1", SHA: "ddddddd1", Continues: a.N})
	if err != nil || !fix.Head || fix.Continues != a.N {
		t.Fatalf("merger's request: %+v %v", fix, err)
	}
	next, ok, err := s.MergeNext(mqRepo)
	if err != nil || !ok || next.N != fix.N {
		t.Fatalf("the merger's request goes first: %+v %v %v", next, ok, err)
	}
}

func TestMergeMoveFollowsTheTableAndNeverLeavesAnEnd(t *testing.T) {
	s := open(t)
	a := mqAsk(t, s, "claude/a", "aaaaaaa1")
	if _, err := s.MergeMove(MergeStep{N: a.N, To: MergeLanded}); !errors.Is(err, ErrMergeMove) || !IsRefusal(err) {
		t.Fatalf("queued to landed: %v", err)
	}
	if _, err := s.MergeMove(MergeStep{N: 99, To: MergeMerging}); !errors.Is(err, ErrMergeNotFound) {
		t.Fatalf("missing: %v", err)
	}
	s.MergeMove(MergeStep{N: a.N, To: MergeMerging, Base: "0000000"})
	m, err := s.MergeMove(MergeStep{N: a.N, To: MergeTesting, Candidate: "1111111", Room: "SG4"})
	if err != nil || m.Candidate != "1111111" || m.Room != "sg4" || m.StartedAt == "" {
		t.Fatalf("testing: %+v %v", m, err)
	}
	if _, err := s.MergeMove(MergeStep{N: a.N, From: MergeMerging, To: MergeLanded}); !errors.Is(err, ErrMergeMove) {
		t.Fatalf("a stale from: %v", err)
	}
	r, err := s.MergeMove(MergeStep{N: a.N, To: MergeQueued, Reason: "claude/main moved"})
	if err != nil || r.Retries != 1 || r.Candidate != "" || r.StartedAt != "" || r.EndedAt != "" {
		t.Fatalf("requeue: %+v %v", r, err)
	}
	s.MergeMove(MergeStep{N: a.N, To: MergeMerging, Base: "2222222"})
	s.MergeMove(MergeStep{N: a.N, To: MergeTesting, Candidate: "3333333"})
	red, err := s.MergeMove(MergeStep{N: a.N, To: MergeRed, Failing: []string{"TestA", "TestB"}, Output: "--- FAIL: TestA\n"})
	if err != nil || red.TestedAt == "" || red.EndedAt == "" || len(red.Failing) != 2 {
		t.Fatalf("red: %+v %v", red, err)
	}
	for _, to := range []string{MergeQueued, MergeMerging, MergeLanded, MergeCancelled} {
		if _, err := s.MergeMove(MergeStep{N: a.N, To: to}); !errors.Is(err, ErrMergeMove) {
			t.Errorf("red to %s: %v", to, err)
		}
	}
	if _, existing, err := s.MergeAsked(MergeAsk{Repo: mqRepo, Branch: "claude/a", SHA: "eeeeeee1"}); err != nil || existing {
		t.Fatalf("a branch whose request ended can ask again: %v %v", existing, err)
	}
}

func TestMergeListKeepsTheOutputOutUnlessAskedAndPutsOpenFirst(t *testing.T) {
	s := open(t)
	a := mqAsk(t, s, "claude/a", "aaaaaaa1")
	mqAsk(t, s, "claude/b", "bbbbbbb1")
	s.MergeMove(MergeStep{N: a.N, To: MergeMerging, Base: "0000000"})
	s.MergeMove(MergeStep{N: a.N, To: MergeTesting, Candidate: "1111111"})
	s.MergeMove(MergeStep{N: a.N, To: MergeLanded, Landed: "1111111", Output: "ok\n"})
	mqAsk(t, s, "claude/c", "ccccccc1")
	got, err := s.MergeList(MergeFilter{Repo: mqRepo})
	if err != nil || len(got) != 3 || got[0].Branch != "claude/c" || got[1].Branch != "claude/b" || got[2].Output != "" {
		t.Fatalf("list: %+v %v", got, err)
	}
	open, _ := s.MergeList(MergeFilter{Open: true})
	if len(open) != 2 {
		t.Fatalf("open: %+v", open)
	}
	full, _ := s.MergeList(MergeFilter{Branch: "claude/a", Output: true})
	if len(full) != 1 || full[0].Output != "ok\n" {
		t.Fatalf("with output: %+v", full)
	}
}

func TestMergeCutKeepsTheHeadAndTheTail(t *testing.T) {
	long := "first line\n" + strings.Repeat("x\n", MergeOutputMax) + "--- FAIL: TestLast\n"
	got := MergeCut(long)
	if len(got) > MergeOutputMax || !strings.HasPrefix(got, "first line\n") || !strings.HasSuffix(got, "--- FAIL: TestLast\n") ||
		!strings.Contains(got, "bytes cut") {
		t.Fatalf("cut: %d bytes, head %q, tail %q", len(got), got[:20], got[len(got)-30:])
	}
	if MergeCut("short") != "short" {
		t.Fatal("a short output must be kept as it is")
	}
}

func TestSetMergeRoomTakesOnlyARoomTheHubHas(t *testing.T) {
	s := open(t)
	if err := s.SetMergeRoom("nowhere"); !IsRefusal(err) {
		t.Fatalf("unknown room: %v", err)
	}
	added(t, s, "SG4")
	if err := s.SetMergeRoom("SG4"); err != nil {
		t.Fatal(err)
	}
	if r, _ := s.MergeRoom(); r != "sg4" {
		t.Fatalf("room: %q", r)
	}
	if err := s.SetMergeRoom(""); err != nil {
		t.Fatal(err)
	}
}
