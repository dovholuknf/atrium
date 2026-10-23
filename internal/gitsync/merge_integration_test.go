//go:build integration && git

package gitsync

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func mergeRig(t *testing.T) *recvFix {
	t.Helper()
	if testing.Short() {
		t.Skip("slow: git subprocesses and a real smart-HTTP rig")
	}
	x := newRecv(t)
	x.seedMain(hubRepo)
	git(t, x.work, "switch", "-q", "-C", "cm", "main")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	return x
}

func TestMergeCandidateMergesAndTheHubLandsItAsAFastForward(t *testing.T) {
	x := mergeRig(t)
	st := x.h.Store()
	git(t, x.work, "switch", "-q", "-C", "a", "cm")
	a := commit(t, x.work, "a.txt", "a")
	x.must(x.push(operator, hubRepo, "a:refs/heads/claude/a"))
	git(t, x.work, "switch", "-q", "cm")
	moved := commit(t, x.work, "m.txt", "m")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))

	r, err := st.MergeCandidate(bg, hubRepo, "claude/a", a[:9], 7, "Merge claude/a (#7)")
	if err != nil || r.Base != moved || r.Tip != a || len(r.Conflicts) != 0 || r.Candidate == "" || r.Candidate == a {
		t.Fatalf("candidate: %+v %v", r, err)
	}
	if got := x.refOn(hubRepo, MergeRef(7)); got != r.Candidate {
		t.Fatalf("the candidate ref is %q", got)
	}
	if err := st.MergeLand(bg, hubRepo, 7, r.Base, r.Candidate); err != nil {
		t.Fatal(err)
	}
	if got := x.refOn(hubRepo, ClaudeMainRef); got != r.Candidate {
		t.Fatalf("claude/main is %s, want %s", got, r.Candidate)
	}
	if err := st.MergeLand(bg, hubRepo, 7, r.Base, r.Candidate); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second land from the old base: %v", err)
	}
	if ok, err := st.DeleteLanded(bg, hubRepo, "claude/a", a); err != nil || !ok || x.refOn(hubRepo, headsPrefix+"claude/a") != "" {
		t.Fatalf("delete: %v %v", ok, err)
	}
	if err := st.MergeDrop(bg, hubRepo, 7); err != nil || x.refOn(hubRepo, MergeRef(7)) != "" {
		t.Fatalf("drop: %v", err)
	}
}

func TestMergeCandidateNamesTheConflictAndWritesNothing(t *testing.T) {
	x := mergeRig(t)
	st := x.h.Store()
	git(t, x.work, "switch", "-q", "-C", "b", "cm")
	b := commit(t, x.work, "same.txt", "from b")
	x.must(x.push(operator, hubRepo, "b:refs/heads/claude/b"))
	git(t, x.work, "switch", "-q", "cm")
	commit(t, x.work, "same.txt", "from main")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))

	r, err := st.MergeCandidate(bg, hubRepo, "claude/b", b, 8, "Merge claude/b (#8)")
	if err != nil || r.Candidate != "" || len(r.Conflicts) != 1 || r.Conflicts[0] != "same.txt" {
		t.Fatalf("conflict: %+v %v", r, err)
	}
	if x.refOn(hubRepo, MergeRef(8)) != "" {
		t.Fatal("a conflict wrote a candidate")
	}
}

func TestMergeCandidateFastForwardsAndKnowsWhatIsAlreadyIn(t *testing.T) {
	x := mergeRig(t)
	st := x.h.Store()
	base := x.refOn(hubRepo, ClaudeMainRef)
	git(t, x.work, "switch", "-q", "-C", "c", "cm")
	c := commit(t, x.work, "c.txt", "c")
	x.must(x.push(operator, hubRepo, "c:refs/heads/claude/c"))
	r, err := st.MergeCandidate(bg, hubRepo, "claude/c", c, 9, "Merge claude/c (#9)")
	if err != nil || r.Candidate != c || r.Base != base {
		t.Fatalf("fast-forward: %+v %v", r, err)
	}
	git(t, x.work, "switch", "-q", "-C", "old", base)
	x.must(x.push(operator, hubRepo, "old:refs/heads/claude/old"))
	if r, err := st.MergeCandidate(bg, hubRepo, "claude/old", "", 10, "x"); err != nil || !r.Already || r.Candidate != "" {
		t.Fatalf("already in: %+v %v", r, err)
	}
	if ok, err := st.DeleteLanded(bg, hubRepo, "claude/c", c); err != nil || ok {
		t.Fatalf("a branch claude/main lacks was deleted: %v %v", ok, err)
	}
}

func TestMergeCandidateRefusesWhatWasNotPushed(t *testing.T) {
	x := mergeRig(t)
	st := x.h.Store()
	if _, err := st.MergeCandidate(bg, hubRepo, "claude/none", "", 1, "x"); err == nil || !strings.Contains(err.Error(), "push it first") {
		t.Fatalf("not pushed: %v", err)
	}
	git(t, x.work, "switch", "-q", "-C", "d", "cm")
	commit(t, x.work, "d.txt", "d")
	x.must(x.push(operator, hubRepo, "d:refs/heads/claude/d"))
	if _, err := st.MergeCandidate(bg, hubRepo, "claude/d", "abcdef1", 1, "x"); err == nil || !strings.Contains(err.Error(), "push again") {
		t.Fatalf("another sha: %v", err)
	}
	st.GitVersion = func(_ context.Context) (string, error) { return "git version 2.37.1", nil }
	if err := st.MergeGitOK(bg); err == nil || !strings.Contains(err.Error(), "2.38") {
		t.Fatalf("old git: %v", err)
	}
}
