//go:build integration && git

package gitsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
	if err := st.MergeLand(bg, hubRepo, 7, r.Base, r.Candidate); err != nil {
		t.Fatalf("the same land again: %v", err)
	}
	if err := st.MergeLand(bg, hubRepo, 8, r.Base, a); !errors.Is(err, ErrConflict) {
		t.Fatalf("another land from the old base: %v", err)
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

func TestMergeLandCutPutsClaudeMainsTreeOnMainAndLeavesClaudeMain(t *testing.T) {
	x := mergeRig(t)
	st := x.h.Store()
	dir, _ := st.Path(hubRepo)
	at := func(f, rev string) string {
		return strings.TrimSpace(must(Default.Git(bg, dir, "log", "-1", "--format="+f, rev)))
	}
	by := LandAuthor{Name: "o", Email: "o@o"}
	main := x.refOn(hubRepo, MainRef)
	c, err := st.MergeLandCut(bg, hubRepo, 1, by, "Land 1: #1 a")
	if err != nil || c.Moved || c.Commit != main || x.refOn(hubRepo, MainRef) != main || x.refOn(hubRepo, LandRefPrefix+"1") != main {
		t.Fatalf("main has claude/main's tree already: %+v %v", c, err)
	}

	git(t, x.work, "switch", "-q", "cm")
	commit(t, x.work, "a.txt", "a")
	old := commit(t, x.work, "b.txt", "b")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	c, err = st.MergeLandCut(bg, hubRepo, 2, by, "Land 2: #2 b")
	if err != nil || !c.Moved || c.MainBefore != main || c.ClaudeBefore != old {
		t.Fatalf("cut: %+v %v", c, err)
	}
	if at("%T", c.Commit) != at("%T", old) || at("%P", c.Commit) != main || at("%an <%ae>|%B", c.Commit) != "o <o@o>|Land 2: #2 b" {
		t.Fatalf("the land commit: %s %s", at("%P", c.Commit), at("%an <%ae>|%B", c.Commit))
	}
	if x.refOn(hubRepo, MainRef) != c.Commit || x.refOn(hubRepo, ClaudeMainRef) != old || x.refOn(hubRepo, LandRefPrefix+"2") != c.Commit {
		t.Fatalf("refs: %s", x.refsOn(hubRepo))
	}
	if strings.Contains(x.refsOn(hubRepo), "refs/heads/land/") {
		t.Fatalf("a land branch: %s", x.refsOn(hubRepo))
	}
	if strings.Contains(must(Default.Git(bg, dir, "cat-file", "-p", c.Commit)), "gpgsig") {
		t.Fatal("the land is signed")
	}

	commit(t, x.work, "c.txt", "c")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	next, err := st.MergeLandCut(bg, hubRepo, 3, by, "Land 3: #3 c")
	if err != nil || !next.Moved || at("%P", next.Commit) != c.Commit || x.refOn(hubRepo, MainRef) != next.Commit {
		t.Fatalf("a land onto the last land: %+v %v", next, err)
	}

	env := []string{"GIT_AUTHOR_NAME=s", "GIT_AUTHOR_EMAIL=s@s", "GIT_COMMITTER_NAME=s", "GIT_COMMITTER_EMAIL=s@s"}
	stray := strings.TrimSpace(must(Default.GitEnv(bg, dir, env, "commit-tree", next.Commit+"^{tree}", "-p", next.Commit, "-m", "stray")))
	must(Default.Git(bg, dir, "update-ref", MainRef, stray))
	if _, err := st.MergeLandCut(bg, hubRepo, 4, by, "Land 4"); !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "neither") {
		t.Fatalf("main moved by hand: %v", err)
	}
}

func must(s string, err error) string {
	if err != nil {
		panic(err)
	}
	return s
}

type mirrorLand struct {
	st         *Store
	ck, bare   string
	base, cand string
	h          *Hub
}

func mirrorLandRig(t *testing.T) *mirrorLand {
	t.Helper()
	if testing.Short() {
		t.Skip("slow: git subprocesses")
	}
	x := &mirrorLand{ck: filepath.ToSlash(t.TempDir())}
	x.h = &Hub{Dir: t.TempDir(), Runner: NewRunner()}
	x.h.Repos = func() ([]Repo, error) {
		return []Repo{{Name: hubRepo, Checkout: x.ck, Branch: "claude/main"}}, nil
	}
	git(t, x.ck, "init", "-q", "-b", "claude/main")
	x.base = commit(t, x.ck, "base.txt", "base")
	if _, err := x.h.Mirror(bg); err != nil {
		t.Fatal(err)
	}
	x.bare = x.h.Bare(hubRepo)
	if err := writeMarker(x.bare, KindAdopted); err != nil {
		t.Fatal(err)
	}
	git(t, x.ck, "switch", "-q", "-c", "cand")
	x.cand = commit(t, x.ck, "cand.txt", "cand")
	git(t, x.ck, "switch", "-q", "claude/main")
	git(t, x.bare, "fetch", "-q", x.ck, "refs/heads/cand:refs/atrium/merge/7")
	x.st = x.h.Store()
	return x
}

func TestMergeLandOnAMirrorFastForwardsTheCheckoutToTheTestedCandidate(t *testing.T) {
	x := mirrorLandRig(t)
	if err := x.st.MergeLand(bg, hubRepo, 7, x.base, x.cand); err != nil {
		t.Fatal(err)
	}
	if got := git(t, x.ck, "rev-parse", "claude/main"); got != x.cand {
		t.Fatalf("the checkout's claude/main is %s, want %s", got, x.cand)
	}
	if got := git(t, x.bare, "rev-parse", ClaudeMainRef); got != x.cand {
		t.Fatalf("the hub's claude/main is %s, want %s", got, x.cand)
	}
	if got := git(t, x.ck, "show", "claude/main:cand.txt"); got != "cand" {
		t.Fatalf("the worktree was not moved: %q", got)
	}
	if moved, err := x.h.Mirror(bg); err != nil || len(moved) != 0 {
		t.Fatalf("the pass after a land: %v %v", moved, err)
	}
}

func TestMergeLandOnAMirrorRefusesADirtyCheckoutAndLeavesBothTips(t *testing.T) {
	x := mirrorLandRig(t)
	if err := os.WriteFile(filepath.Join(x.ck, "base.txt"), []byte("edited"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := x.st.MergeLand(bg, hubRepo, 7, x.base, x.cand)
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "not committed") || !strings.Contains(err.Error(), "the operator lands it") {
		t.Fatalf("a dirty checkout: %v", err)
	}
	if got := git(t, x.ck, "rev-parse", "claude/main"); got != x.base {
		t.Fatalf("the checkout moved to %s", got)
	}
	if got := git(t, x.bare, "rev-parse", ClaudeMainRef); got != x.base {
		t.Fatalf("the hub's claude/main was left at %s", got)
	}
}

func TestMergeLandOnAMirrorIsAConflictWhenTheCheckoutsTipMoved(t *testing.T) {
	x := mirrorLandRig(t)
	commit(t, x.ck, "other.txt", "other")
	if err := x.st.MergeLand(bg, hubRepo, 7, x.base, x.cand); !errors.Is(err, ErrConflict) {
		t.Fatalf("a moved checkout: %v", err)
	}
	if got := git(t, x.bare, "rev-parse", ClaudeMainRef); got != x.base {
		t.Fatalf("the hub's claude/main moved to %s", got)
	}
}
