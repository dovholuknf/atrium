//go:build integration && git

package gitsync

import (
	"context"
	"strings"
	"testing"
)

// The landed rule over a plain temp repository: no hub, no store, only a Runner and a directory.

// landedHow is what branch status concludes of tip against ref, by the same steps: the commits of tip that ref lacks,
// each read once, each carried or not. how is the weakest evidence, ancestry when there is nothing beyond ref.
func landedHow(t *testing.T, x *bsFix, ref, tip string) (how string, unlanded int) {
	t.Helper()
	ctx := context.Background()
	r := NewRunner()
	list := func(args ...string) []string {
		out, err := r.Git(ctx, x.work, append([]string{"rev-list", "--no-merges"}, args...)...)
		if err != nil {
			t.Fatal(err)
		}
		return strings.Fields(out)
	}
	mainFacts, err := readFacts(ctx, r, x.work, nil, list(ref))
	if err != nil {
		t.Fatal(err)
	}
	pids, subj := map[string]bool{}, map[string]map[string]bool{}
	for _, f := range mainFacts {
		if f.pid != "" {
			pids[f.pid] = true
		}
		if subj[f.subject] == nil {
			subj[f.subject] = map[string]bool{}
		}
		subj[f.subject][f.key] = true
	}
	own := list(tip, "^"+ref)
	facts, err := readFacts(ctx, r, x.work, nil, own)
	if err != nil {
		t.Fatal(err)
	}
	if len(own) == 0 {
		return HowAncestry, 0
	}
	for _, c := range own {
		if c := carriedBy(facts[c], pids, subj); c == "" {
			unlanded++
		} else {
			how = weaker(how, c)
		}
	}
	return how, unlanded
}

// landedFix has a base commit on `main` and a branch `work` cut from it with one commit, `msg` touching a.txt.
func landedFix(t *testing.T) *bsFix {
	t.Helper()
	x := newBranchFix(t)
	x.commit("base", map[string]string{"base.txt": "base\n"})
	x.branch("main", "HEAD")
	return x
}

func TestLandedFastForwardReadsAncestry(t *testing.T) {
	x := landedFix(t)
	x.branch("work", "main")
	x.commit("work one", map[string]string{"a.txt": "a\n"})
	x.branch("main", "work")
	if how, n := landedHow(t, x, "main", "work"); how != HowAncestry || n != 0 {
		t.Fatalf("how %q unlanded %d", how, n)
	}
}

func TestLandedCherryPickReadsPatch(t *testing.T) {
	x := landedFix(t)
	x.branch("work", "main")
	c := x.commit("work one", map[string]string{"a.txt": "a\n"})
	x.branch("main", "main")
	x.git("cherry-pick", c)
	if how, n := landedHow(t, x, "main", "work"); how != HowPatchID || n != 0 {
		t.Fatalf("how %q unlanded %d", how, n)
	}
}

func TestLandedRewordedCherryPickWithSameFilesReadsSubject(t *testing.T) {
	x := landedFix(t)
	x.branch("work", "main")
	x.commit("same subject", map[string]string{"a.txt": "a\n"})
	x.branch("main", "main")
	// The same subject and the same file, but not the same change, so no patch id matches.
	x.commit("same subject", map[string]string{"a.txt": "a, edited by hand\n"})
	if how, n := landedHow(t, x, "main", "work"); how != HowSubject || n != 0 {
		t.Fatalf("how %q unlanded %d", how, n)
	}
}

func TestLandedSameSubjectOtherFilesReadsUnlanded(t *testing.T) {
	x := landedFix(t)
	x.branch("work", "main")
	x.commit("Fix tests", map[string]string{"a.txt": "a\n"})
	x.branch("main", "main")
	x.commit("Fix tests", map[string]string{"other.txt": "o\n"})
	if how, n := landedHow(t, x, "main", "work"); how != "" || n != 1 {
		t.Fatalf("how %q unlanded %d", how, n)
	}
}

func TestLandedMergeCommitOnTheBranchIsIgnored(t *testing.T) {
	x := landedFix(t)
	x.branch("side", "main")
	x.commit("side work", map[string]string{"s.txt": "s\n"})
	x.branch("work", "main")
	c := x.commit("work one", map[string]string{"a.txt": "a\n"})
	x.git("merge", "-q", "--no-ff", "-m", "merge side", "side")
	x.branch("main", "side")
	x.git("cherry-pick", c)
	if how, n := landedHow(t, x, "main", "work"); how != HowPatchID || n != 0 {
		t.Fatalf("how %q unlanded %d", how, n)
	}
}

// Branch status counts a branch landed when its changelog fragment is on main, which a squash can leave behind with
// the code dropped. The commit rules alone do not carry such a branch.
func TestLandedChangelogFragmentAloneReadsUnlanded(t *testing.T) {
	x := landedFix(t)
	x.branch("work", "main")
	x.commit("work one", map[string]string{"a.txt": "a\n", "changelog/ops/2026-10-16-x.md": "x\n"})
	x.branch("main", "main")
	x.commit("squash of other things", map[string]string{"changelog/ops/2026-10-16-x.md": "x\n"})
	if how, n := landedHow(t, x, "main", "work"); how != "" || n != 1 {
		t.Fatalf("how %q unlanded %d", how, n)
	}
}
