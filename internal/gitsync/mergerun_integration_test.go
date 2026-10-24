//go:build integration && git

package gitsync

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type asRoom struct {
	x    *recvFix
	room string
}

func (a asRoom) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	r.URL.Host = strings.TrimPrefix(a.x.srv.URL, "http://")
	r.Header.Set("X-Test-Caller", "room:"+a.room)
	return http.DefaultTransport.RoundTrip(r)
}

const fakeTouched = `#!/bin/bash
echo "base $2"
if [ -f known.txt ]; then echo "--- FAIL: TestRealSessionsKeepTheirText (2.00s)"; echo "FAILED go test ./internal/daemon"; exit 1; fi
if [ -f fail.txt ]; then echo "--- FAIL: TestBroken (0.01s)"; echo "FAILED go test ./internal/x"; exit 1; fi
echo "test-touched: everything it ran passed"
`

func mergeRoomRig(t *testing.T) (*recvFix, *MergeRunner, string) {
	t.Helper()
	x := mergeRig(t)
	if err := os.MkdirAll(filepath.Join(x.work, "scripts"), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, x.work, "switch", "-q", "cm")
	commit(t, x.work, "scripts/test-touched.sh", fakeTouched)
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))
	root := t.TempDir()
	clone := filepath.Join(root, "github", "o", "r")
	if err := os.MkdirAll(filepath.Dir(clone), 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, "", "clone", "-q", x.work, clone)
	m := &MergeRunner{Root: func() string { return root }, Dir: filepath.Join(t.TempDir(), "merge"),
		Hub: func() (http.RoundTripper, error) { return asRoom{x, "sg4"}, nil }, Runner: x.h.Runner}
	return x, m, clone
}

func TestMergeRunnerTestsTheCandidateAndNamesWhatFailed(t *testing.T) {
	x, m, _ := mergeRoomRig(t)
	st := x.h.Store()
	git(t, x.work, "switch", "-q", "-C", "good", "cm")
	commit(t, x.work, "good.txt", "good")
	x.must(x.push(operator, hubRepo, "good:refs/heads/claude/good"))
	git(t, x.work, "switch", "-q", "cm")
	commit(t, x.work, "other.txt", "other")
	x.must(x.push(operator, hubRepo, "cm:refs/heads/claude/main"))

	r, err := st.MergeCandidate(bg, hubRepo, "claude/good", "", 1, "Merge claude/good (#1)")
	if err != nil || r.Candidate == "" {
		t.Fatalf("candidate: %+v %v", r, err)
	}
	rep := m.Run(bg, MergeJob{N: 1, Repo: hubRepo, Candidate: r.Candidate, Base: r.Base})
	if rep.Verdict != VerdictGreen || !strings.Contains(rep.Output, "base "+r.Base) {
		t.Fatalf("green: %+v", rep)
	}

	git(t, x.work, "switch", "-q", "-C", "bad", "cm")
	commit(t, x.work, "fail.txt", "fail")
	x.must(x.push(operator, hubRepo, "bad:refs/heads/claude/bad"))
	r, err = st.MergeCandidate(bg, hubRepo, "claude/bad", "", 2, "Merge claude/bad (#2)")
	if err != nil {
		t.Fatal(err)
	}
	rep = m.Run(bg, MergeJob{N: 2, Repo: hubRepo, Candidate: r.Candidate, Base: r.Base})
	if rep.Verdict != VerdictRed || len(rep.Failing) != 2 || rep.Failing[0] != "TestBroken" ||
		rep.Failing[1] != "go test ./internal/x" {
		t.Fatalf("red: %+v", rep)
	}
	rep = m.Run(bg, MergeJob{N: 1, Repo: hubRepo, Candidate: strings.Repeat("a", 40), Base: r.Base})
	if rep.Verdict != VerdictCannot || !strings.Contains(rep.Reason, "candidate for run 1 is not") {
		t.Fatalf("a candidate that is not the hub's: %+v", rep)
	}
}

func TestMergeRunnerPrunesOnlyALandedBranchNobodyHasOut(t *testing.T) {
	_, m, clone := mergeRoomRig(t)
	tip := git(t, clone, "rev-parse", "HEAD")
	git(t, clone, "branch", "claude/done", tip)
	git(t, clone, "branch", "claude/out", tip)
	git(t, clone, "worktree", "add", "-q", filepath.Join(t.TempDir(), "w"), "claude/out")
	if ok, why, err := m.Prune(bg, MergePrune{Repo: hubRepo, Branch: "claude/done", SHA: tip}); err != nil || !ok {
		t.Fatalf("prune: %v %q %v", ok, why, err)
	}
	if ok, why, err := m.Prune(bg, MergePrune{Repo: hubRepo, Branch: "claude/out", SHA: tip}); err != nil || ok ||
		!strings.Contains(why, "checked out") {
		t.Fatalf("checked out: %v %q %v", ok, why, err)
	}
	if ok, why, _ := m.Prune(bg, MergePrune{Repo: hubRepo, Branch: "claude/out", SHA: strings.Repeat("b", 40)}); ok ||
		!strings.Contains(why, "not the landed") {
		t.Fatalf("another sha: %v %q", ok, why)
	}
	if _, _, err := m.Prune(bg, MergePrune{Repo: hubRepo, Branch: "claude/main", SHA: tip}); err == nil {
		t.Fatal("claude/main was considered")
	}
}

func TestMergeFailingNamesEachOnceInOrder(t *testing.T) {
	out := "--- FAIL: TestA (0.1s)\n    --- FAIL: TestA/sub (0.0s)\nFAIL\tgithub.com/x/y\t1.2s\n--- FAIL: TestA (0.1s)\nFAILED node scripts/z.js\n"
	got := strings.Join(MergeFailing(out), ",")
	if got != "TestA,TestA/sub,github.com/x/y,node scripts/z.js" {
		t.Fatalf("failing: %s", got)
	}
}

func TestMergeFailingCleansAColouredName(t *testing.T) {
	out := "\x1b[31m--- FAIL: TestC (0.1s)\x1b[0m\r\nFAILED \x1b[1mnode x.js\x1b[0m\r\n"
	if got := strings.Join(MergeFailing(out), ","); got != "TestC,node x.js" {
		t.Fatalf("failing: %q", got)
	}
}

func TestMergeRunnerPassesARedOfOnlyKnownFailures(t *testing.T) {
	x, m, _ := mergeRoomRig(t)
	st := x.h.Store()
	git(t, x.work, "switch", "-q", "-C", "known", "cm")
	if err := os.MkdirAll(filepath.Join(x.work, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	commit(t, x.work, "docs/known-failing-tests.md", "# Known failing\n\n- `TestRealSessionsKeepTheirText` flakes\n")
	commit(t, x.work, "known.txt", "known")
	x.must(x.push(operator, hubRepo, "known:refs/heads/claude/known"))
	r, err := st.MergeCandidate(bg, hubRepo, "claude/known", "", 3, "Merge claude/known (#3)")
	if err != nil || r.Candidate == "" {
		t.Fatalf("candidate: %+v %v", r, err)
	}
	rep := m.Run(bg, MergeJob{N: 3, Repo: hubRepo, Candidate: r.Candidate, Base: r.Base})
	if rep.Verdict != VerdictGreen || !strings.Contains(rep.Note, KnownFailingFile) {
		t.Fatalf("known: %+v", rep)
	}
}
