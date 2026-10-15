package gitsync

import (
	"context"
	"strings"
	"sync"
)

// WHAT THE HUB'S MIRROR SAYS OF AN ITEM'S TESTS AND REVIEWS (item lifecycle L4). A test is a fact pinned to a commit, so what
// the lifecycle needs of git is two things: how many commits the branch has moved since each commit a test names, and what
// the verdict trailers say of the item's own commits. Both are read through internal/deployready, so a verdict means here what
// it means to a deploy. gitsync cannot import deployready (deployready's own tests use gitsync), so the checker comes in through
// Store.Checker, which the hub's wiring sets. See docs/rnd/item-lifecycle-design.md, stage L4.

// Evidence is what the mirror says of one item's branch.
type Evidence struct {
	// Tip is the branch tip, empty when the hub holds no such branch.
	Tip string `json:"tip"`
	// Base is where the item's own commits begin: the fork point from claude/main. Empty when the branch is already on it.
	Base string `json:"base,omitempty"`
	// Since is, for each sha asked about, how many commits of work the branch has gained after it. -1 says the sha is not on
	// the branch's line any more (rebased away) or the hub does not hold it.
	Since map[string]int `json:"since,omitempty"`
	// Cover is what the verdict trailers say of base..tip. CoverKnown is false when it could not be read, or when the branch
	// has nothing of its own (it is already on claude/main).
	Cover      Coverage `json:"cover"`
	CoverKnown bool     `json:"cover_known"`
}

// Coverage is how much of a range the verdicts cover: the part of deployready.Coverage the lifecycle reads.
type Coverage struct {
	Total      int      `json:"total"`
	Covered    int      `json:"covered"`
	Held       []string `json:"held,omitempty"`
	TipCovered bool     `json:"tip_covered"`
}

// EvidenceChecker is deployready.Checker, as far as this package needs it.
type EvidenceChecker interface {
	Since(ctx context.Context, from, to string) (int, error)
	Coverage(ctx context.Context, base, tip string, alsoOn ...string) (Coverage, error)
}

// CheckerGit is what a checker runs git through. A *Runner is one.
type CheckerGit interface {
	Git(ctx context.Context, dir string, args ...string) (string, error)
	GitInput(ctx context.Context, dir string, stdin []byte, args ...string) (string, error)
}

// checkers keeps one checker per mirror, so the per-commit patch ids it learns survive from one read to the next.
var checkers sync.Map

func (s *Store) checkerFor(dir string) EvidenceChecker {
	if v, ok := checkers.Load(dir); ok {
		return v.(EvidenceChecker)
	}
	if s.Checker == nil {
		return nil
	}
	c := s.Checker(s.h.runner(), dir, strings.TrimPrefix(ClaudeMainRef, headsPrefix))
	v, _ := checkers.LoadOrStore(dir, c)
	return v.(EvidenceChecker)
}

// ItemEvidence reads the mirror of the repository `name` for an item's branch. A repository or a branch the hub does not hold
// is no evidence and no error. shas are the commits tests were recorded at.
func (s *Store) ItemEvidence(ctx context.Context, name, branch string, shas []string) (Evidence, error) {
	var ev Evidence
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return ev, err
	}
	if why := CheckPushBranch(branch); why != "" {
		return ev, refuse("%s", why)
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	tip, ok, err := s.tipOf(ctx, dir, branch)
	if err != nil || !ok {
		return ev, err
	}
	ev.Tip = tip
	c := s.checkerFor(dir)
	if c == nil {
		return ev, nil
	}
	ev.Since = map[string]int{}
	for _, sha := range shas {
		if _, done := ev.Since[sha]; done || !ValidSHA(sha) {
			continue
		}
		known, err := s.commitKnown(ctx, dir, sha)
		if err != nil || !known {
			ev.Since[sha] = -1
			continue
		}
		n, err := c.Since(ctx, sha, tip)
		if err != nil {
			n = -1
		}
		ev.Since[sha] = n
	}
	base, err := s.git(ctx, dir, "merge-base", tip, ClaudeMainRef)
	if err != nil {
		return ev, nil
	}
	base = strings.TrimSpace(base)
	if !ValidSHA(base) || base == tip {
		return ev, nil
	}
	ev.Base = base
	cov, err := c.Coverage(ctx, base, tip, headsPrefix+branch)
	if err != nil {
		return ev, nil
	}
	ev.Cover, ev.CoverKnown = cov, true
	return ev, nil
}
