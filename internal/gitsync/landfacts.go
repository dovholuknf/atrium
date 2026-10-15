package gitsync

import (
	"context"
	"strings"
)

// WHAT THE HUB'S MIRROR SAYS OF AN ITEM'S LANDING AND PUSHING, for the item lifecycle's `landed` and `pushed` steps and
// for the scope of `deployed`. See docs/rnd/item-lifecycle-design.md, stage L3.
//
//   - LANDED IS ANCESTRY, NOT PATCH-EQUIVALENCE. The build tip is an ancestor of claude/main on the mirror. A rebased or
//     re-signed branch is a different sha and is not landed here, which is the truth about that sha. (ItemFacts in
//     flowfacts.go is the looser read the merge flow wants.)
//   - PUSHED ASKS THE FORGE. One `ls-remote` for its main, and the landed sha is an ancestor of that tip. A forge that
//     cannot be asked, or a tip the mirror has not fetched yet, falls back to the mirror's own main and says so.

// LandFacts is what the mirror says of one item's branch.
type LandFacts struct {
	// SHA is the branch tip the facts are about, empty when the hub holds neither the branch nor the sha.
	SHA string `json:"sha"`
	// Landed is that SHA is an ancestor of claude/main, and LandedSHA is the first commit on claude/main that holds it:
	// SHA itself for a fast-forward, the merge commit otherwise.
	Landed    bool   `json:"landed"`
	LandedSHA string `json:"landed_sha,omitempty"`
	// Pushed is that origin's main holds LandedSHA. PushedVia is `origin` when the forge was asked and `mirror` when
	// the mirror's own main had to answer. PushedNote says why it had to.
	Pushed     bool   `json:"pushed"`
	PushedVia  string `json:"pushed_via,omitempty"`
	PushedNote string `json:"pushed_note,omitempty"`
	// Paths are the files the item's own commits changed, which is what decides where it deploys.
	Paths []string `json:"paths,omitempty"`
}

// LandFacts reads the mirror of the repository `name` for the item's branch, or for sha when the branch is empty. workID is the item's backlog id, which finds its commits once the branch is on main. A
// repository the hub does not hold is no facts and no error.
func (s *Store) LandFacts(ctx context.Context, name, branch, sha, workID string) (LandFacts, error) {
	var f LandFacts
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return f, err
	}
	ctx, cancel := context.WithTimeout(ctx, s.forgeBound()+readBound)
	defer cancel()
	if branch != "" {
		if why := CheckPushBranch(branch); why != "" {
			return f, refuse("%s", why)
		}
		tip, ok, err := s.tipOf(ctx, dir, branch)
		if err != nil {
			return f, err
		}
		if ok {
			sha = tip
		}
	}
	if !ValidSHA(sha) {
		return f, nil
	}
	if known, err := s.commitKnown(ctx, dir, sha); err != nil || !known {
		return f, err
	}
	f.SHA = sha
	if f.Landed, err = s.ancestorOfRef(ctx, dir, sha, ClaudeMainRef); err != nil || !f.Landed {
		f.Paths, _ = s.itemPaths(ctx, dir, sha, workID)
		return f, err
	}
	if f.LandedSHA, err = s.firstContaining(ctx, dir, sha, ClaudeMainRef); err != nil {
		return f, err
	}
	f.Paths, err = s.itemPaths(ctx, dir, sha, workID)
	if err != nil {
		return f, err
	}
	f.Pushed, f.PushedVia, f.PushedNote = s.pushedToOrigin(ctx, dir, name, f.LandedSHA)
	return f, nil
}

// ancestorOfRef is isAncestor against a ref the repository may not have, which holds nothing.
func (s *Store) ancestorOfRef(ctx context.Context, dir, sha, ref string) (bool, error) {
	if _, err := s.git(ctx, dir, "rev-parse", "--verify", "-q", ref+"^{commit}"); err != nil {
		if exitCode(err) == 1 {
			return false, nil
		}
		return false, err
	}
	return s.isAncestor(ctx, dir, sha, ref)
}

// firstContaining is the first commit on ref's first-parent line that holds sha. When sha is on that line it is sha
// (a fast-forward), and otherwise the merge that brought it in.
func (s *Store) firstContaining(ctx context.Context, dir, sha, ref string) (string, error) {
	out, err := s.git(ctx, dir, "rev-list", "--reverse", "--first-parent", "--ancestry-path", sha+".."+ref)
	if err != nil {
		return "", err
	}
	first := strings.Fields(out)
	if len(first) == 0 {
		return sha, nil
	}
	parents, err := s.git(ctx, dir, "rev-list", "--parents", "-n", "1", first[0])
	if err != nil {
		return "", err
	}
	if f := strings.Fields(parents); len(f) > 1 && f[1] == sha {
		return sha, nil
	}
	return first[0], nil
}

// itemPaths are the files the item's own commits changed: what the branch holds that its fork point from origin's main
// does not. Once the branch is on main that range is empty, so the commits that carry the item's changelog entry stand
// in, which is how every change here names its item.
func (s *Store) itemPaths(ctx context.Context, dir, sha, workID string) ([]string, error) {
	if base, err := s.git(ctx, dir, "merge-base", sha, MainRef); err == nil {
		if b := strings.TrimSpace(base); ValidSHA(b) && b != sha {
			return s.pathsOf(ctx, dir, "--no-merges", sha, "^"+b)
		}
	}
	if workID == "" || strings.ContainsAny(workID, "*?[]\\ \n") {
		return nil, nil
	}
	return s.pathsOf(ctx, dir, "--no-merges", ClaudeMainRef, "--", "changelog/*/*-"+workID+".md")
}

// pathsOf lists the files changed by the commits `git log <args>` selects, each once.
func (s *Store) pathsOf(ctx context.Context, dir string, args ...string) ([]string, error) {
	out, err := s.git(ctx, dir, append([]string{"log", "--no-renames", "--name-only", "--format=", "--max-count=500"}, args...)...)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var paths []string
	for _, l := range strings.Split(out, "\n") {
		if l = strings.TrimSpace(l); l != "" && !seen[l] {
			seen[l] = true
			paths = append(paths, l)
		}
	}
	return paths, nil
}

// pushedToOrigin asks the forge for its main and says whether it holds landed. A forge that does not answer, or a tip the
// mirror has not got, leaves the mirror's own main to answer, and the third result says why.
func (s *Store) pushedToOrigin(ctx context.Context, dir, name, landed string) (bool, string, string) {
	viaMirror := func(why string) (bool, string, string) {
		ok, err := s.ancestorOfRef(ctx, dir, landed, MainRef)
		if err != nil {
			return false, "mirror", why + ", and the mirror could not be read"
		}
		return ok, "mirror", why
	}
	ref, err := ParseName(name)
	if err != nil {
		return viaMirror("the repository name does not name a forge")
	}
	got, err := s.forgeHeads(ctx, ref, s.helper(ref), []string{"main"})
	if err != nil {
		return viaMirror("the forge did not answer")
	}
	tip, ok := got["main"]
	if !ok {
		return viaMirror("the forge has no main")
	}
	if known, err := s.commitKnown(ctx, dir, tip); err != nil || !known {
		return viaMirror("the mirror has not fetched the forge's main yet")
	}
	up, err := s.isAncestor(ctx, dir, landed, tip)
	if err != nil {
		return viaMirror("the mirror could not compare against the forge's main")
	}
	return up, "origin", ""
}

// Contains says whether the commit `build` holds sha, and known false when the mirror does not have `build`, which is a
// room on a build the hub has never seen: older than anything it holds, as far as anyone can tell.
func (s *Store) Contains(ctx context.Context, name, build, sha string) (contains, known bool, err error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" || !ValidSHA(build) || !ValidSHA(sha) {
		return false, false, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	if known, err = s.commitKnown(ctx, dir, build); err != nil || !known {
		return false, false, err
	}
	contains, err = s.isAncestor(ctx, dir, sha, build)
	return contains, true, err
}
