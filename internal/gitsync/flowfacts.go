package gitsync

import (
	"context"
	"regexp"
	"strings"
)

var shaPrefixRe = regexp.MustCompile(`^[0-9a-f]{7,39}$`)

// What the hub's repository says of a backlog item's work, for the merge flow.
//
// A COMMIT COUNTS AS IN A BRANCH WHEN IT IS AN ANCESTOR OR HAS A PATCH-EQUIVALENT THERE. Re-signing, a rebase and a
// conflict fix all rewrite shas and keep the change, and `git cherry` is the test that sees through that: it lists
// the commits of the work the branch lacks, a `-` for one the branch has by patch id, a `+` for one it does not.
// Nothing listed with a `+` means everything is there.

// ItemFacts is what git says of one item's work.
type ItemFacts struct {
	// SHA is the commit the facts are about: the one given, else the tip of the item's branch. Empty when neither is
	// known to the hub, and then every other field is false.
	SHA string `json:"sha"`
	// Implemented is that the hub holds the work, as a branch tip or as a commit.
	Implemented bool `json:"implemented"`
	// Merged is that claude/main has all of it. PushedByClint is that main, the mirror of origin/main that only the
	// operator moves, has all of it.
	Merged        bool `json:"merged"`
	PushedByClint bool `json:"pushed_by_clint"`
}

// ItemFacts answers for the work of an item in the repository `name`: its branch (may be empty) and its sha (may be
// empty). A repository the hub does not hold is no facts and no error.
func (s *Store) ItemFacts(ctx context.Context, name, branch, sha string) (ItemFacts, error) {
	var f ItemFacts
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return f, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	if sha == "" && branch != "" {
		if why := CheckPushBranch(branch); why != "" {
			return f, refuse("%s", why)
		}
		tip, ok, err := s.tipOf(ctx, dir, branch)
		if err != nil || !ok {
			return f, err
		}
		sha = tip
	}
	if !ValidSHA(sha) {
		// A short sha is looked up whole, so the facts name the commit.
		if !shaPrefixRe.MatchString(sha) {
			return f, nil
		}
		out, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", sha+"^{commit}")
		if err != nil {
			return f, nil
		}
		sha = strings.TrimSpace(out)
	}
	known, err := s.commitKnown(ctx, dir, sha)
	if err != nil || !known {
		return f, err
	}
	f.SHA, f.Implemented = sha, true
	if f.Merged, err = s.covered(ctx, dir, ClaudeMainRef, sha); err != nil {
		return f, err
	}
	f.PushedByClint, err = s.covered(ctx, dir, MainRef, sha)
	return f, err
}

// CoveredBy says whether every commit of `sha` has its change in the commit `base`: it is an ancestor of it, or each
// of its commits has a patch-equivalent there. THIS IS HOW A CHECK ON A SHA OF MAIN, OR A DEPLOY OF ONE, IS TIED TO AN
// ITEM THAT WAS REBASED OR RE-SIGNED ON THE WAY. Either commit the hub does not hold is false.
func (s *Store) CoveredBy(ctx context.Context, name, base, sha string) (bool, error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" || !ValidSHA(base) || !ValidSHA(sha) {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	for _, c := range []string{base, sha} {
		if known, err := s.commitKnown(ctx, dir, c); err != nil || !known {
			return false, err
		}
	}
	return s.covered(ctx, dir, base, sha)
}

// covered says whether every commit of `sha` not reachable from ref has a patch-equivalent in ref. A ref the
// repository lacks covers nothing.
func (s *Store) covered(ctx context.Context, dir, ref, sha string) (bool, error) {
	if _, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil {
		if exitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}
	out, err := s.git(ctx, dir, "cherry", ref, sha)
	if err != nil {
		return false, err
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "+") {
			return false, nil
		}
	}
	return true, nil
}
