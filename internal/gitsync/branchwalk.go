package gitsync

import (
	"context"
	"errors"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
)

// A HUB BRANCH AGAINST ITS REPOSITORY'S MAIN, for the walk with no card. The branch the caller names is only compared
// with the heads the store lists, and git is handed the listed sha, never the caller's text.

// branchWalkMax bounds the patch the walk is handed, as the room's own walk does.
const branchWalkMax = 4 << 20

// branchWalkBases are the heads a branch is compared with, the first one the store holds.
var branchWalkBases = []string{"main", "master"}

// BranchWalk is what GET /_hub/git/walk answers.
type BranchWalk struct {
	Repo      string `json:"repo"`
	Branch    string `json:"branch"`
	Base      string `json:"base"`
	MergeBase string `json:"merge_base"`
	Head      string `json:"head"`
	Diff      string `json:"diff"`
	Truncated bool   `json:"truncated"`
}

// BranchWalk is the patch of `branch` in the repository `name` since it left main. An error is a repository the hub
// does not hold, a branch it does not list, a repository with no main, or a git that failed.
func (s *Store) BranchWalk(ctx context.Context, name, branch string) (BranchWalk, error) {
	out := BranchWalk{Repo: name}
	dir, err := s.bareFor(name)
	if err != nil {
		return out, err
	}
	if dir == "" {
		return out, refuse("the hub holds no repository %s", name)
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	list, err := s.git(ctx, dir, gitexec.ForEachRef{Format: "%(refname) %(objectname)",
		Patterns: []string{"refs/heads/"}})
	if err != nil {
		return out, err
	}
	heads := map[string]string{}
	for _, l := range strings.Split(list, "\n") {
		if n, sha, ok := strings.Cut(strings.TrimSpace(l), " "); ok && ValidSHA(sha) && strings.HasPrefix(n, "refs/heads/") {
			heads[strings.TrimPrefix(n, "refs/heads/")] = sha
		}
	}
	head, ok := heads[branch]
	if !ok {
		return out, refuse("%s has no branch by that name on the hub", name)
	}
	out.Branch, out.Head = branch, head
	for _, b := range branchWalkBases {
		if sha := heads[b]; sha != "" {
			out.Base = b
			break
		}
	}
	if out.Base == "" {
		return out, refuse("%s has no main on the hub to compare with", name)
	}
	mb, err := s.git(ctx, dir, gitexec.MergeBase{A: heads[out.Base], B: head})
	if err != nil {
		return out, refuse("%s shares no history with %s", branch, out.Base)
	}
	out.MergeBase = strings.TrimSpace(mb)
	patch, err := s.h.runner().GitCapped(ctx, dir, s.env(), branchWalkMax,
		gitexec.Diff{Revs: []string{out.MergeBase, head}, NoExtDiff: true, NoTextconv: true, NoColor: true,
			FindRenames: true})
	if errors.Is(err, ErrOutputCap) {
		out.Truncated, err = true, nil
	}
	if err != nil {
		return out, err
	}
	out.Diff = patch
	return out, nil
}
