//go:build integration && git

package deployready

import (
	"context"
	"errors"
	"strings"
	"testing"
)

func TestCoverageCountsOnlyWorkAndSaysWhatIsCleared(t *testing.T) {
	t.Parallel()
	r, base := newRepoAt(t, plainBase)
	one := r.commit("one", map[string]string{"internal/hubstore/a.go": "1"})
	tip := r.commit("two", map[string]string{"internal/hubstore/b.go": "2"})
	c := r.checker()
	cov, err := c.Coverage(context.Background(), base, tip)
	if err != nil || cov.Total != 2 || cov.Covered != 0 || cov.TipCovered {
		t.Fatalf("no verdict: %+v %v", cov, err)
	}
	r.review("rt", "Atrium-Verdict: hub-ok "+base+".."+one)
	cov, err = c.Coverage(context.Background(), base, tip)
	if err != nil || cov.Total != 2 || cov.Covered != 1 || cov.TipCovered || cov.Line() != "covers 1 of 2 commits" {
		t.Fatalf("one of two: %+v %v", cov, err)
	}
}

func TestCoverageReadsVerdictsOnAnItemBranch(t *testing.T) {
	t.Parallel()
	r, base := newRepoAt(t, plainBase)
	r.git("checkout", "-q", "-b", "claude/item")
	tip := r.commit("one", map[string]string{"internal/hubstore/a.go": "1"})
	r.review("rt", "Atrium-Verdict: hub-ok "+base+".."+tip)
	r.git("checkout", "-q", "claude/main")
	c := r.checker()
	cov, err := c.Coverage(context.Background(), base, tip)
	if err != nil || cov.Covered != 0 {
		t.Fatalf("verdict on the branch only, not asked for: %+v %v", cov, err)
	}
	cov, err = c.Coverage(context.Background(), base, tip, "refs/heads/claude/item")
	if err != nil || cov.Total != 1 || cov.Covered != 1 || !cov.TipCovered {
		t.Fatalf("asked for the branch: %+v %v", cov, err)
	}
}

func TestSinceCountsWorkNotReviews(t *testing.T) {
	t.Parallel()
	r, base := newRepoAt(t, plainBase)
	c := r.checker()
	if n, err := c.Since(context.Background(), base, base); err != nil || n != 0 {
		t.Fatalf("same commit: %d %v", n, err)
	}
	r.commit("one", map[string]string{"internal/hubstore/a.go": "1"})
	r.review("rt", "Atrium-Verdict: hub-ok "+base+"..HEAD")
	tip := r.git("rev-parse", "HEAD")
	if n, err := c.Since(context.Background(), base, tip); err != nil || n != 1 {
		t.Fatalf("a work commit and a review commit are 1 commit: %d %v", n, err)
	}
	// a commit that is not on the line is not behind the tip
	r.git("checkout", "-q", "-b", "side", base)
	side := r.commit("side", map[string]string{"internal/s.go": "s"})
	_, err := c.Since(context.Background(), side, tip)
	if !errors.Is(err, ErrNotBehind) || !strings.Contains(err.Error(), "ancestor") {
		t.Fatalf("a sibling: %v", err)
	}
}
