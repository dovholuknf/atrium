package gitsync

import (
	"context"
	"fmt"
	"strconv"
	"strings"
)

// The merge queue's git, in the hub's own store. See docs/rnd/merge-queue.md.
//
// THE HUB MERGES AND NEVER CHECKS OUT. `git merge-tree --write-tree` makes the merged tree from objects the store
// already has, and `commit-tree` the commit, so no working tree is made and no script of the repository runs here.
// The candidate is kept under refs/atrium/merge/<n>, which no push can reach and a room fetches like a PR head.
//
// claude/main IS MOVED BY THE HUB, UNDER THE LOCK EVERY PUSH HOLDS, and only from the tip the candidate was merged
// onto, so a push that landed in between is a refusal and never a lost commit.

// MergeRefPrefix is where a request's candidate is kept until the request ends.
const MergeRefPrefix = "refs/atrium/merge/"

// MergeGit is the oldest git with `merge-tree --write-tree`.
const MergeGit = "2.38"

// The identity of a merge commit the queue makes. A land rewrites it.
const (
	mergeName  = "atrium merge queue"
	mergeEmail = "merge-queue@atrium.invalid"
)

// MergeResult is what one merge in the store came to.
type MergeResult struct {
	// Base is the claude/main the branch was merged onto, and Tip the branch's tip.
	Base string `json:"base"`
	Tip  string `json:"tip"`
	// Candidate is the commit claude/main would become. Empty on a conflict and when Already.
	Candidate string `json:"candidate,omitempty"`
	// Conflicts names the conflicted paths. Non-empty means the merge did not apply.
	Conflicts []string `json:"conflicts,omitempty"`
	// Already is that claude/main has the branch already, so there is nothing to land.
	Already bool `json:"already,omitempty"`
}

// MergeRef is the candidate's ref of request n.
func MergeRef(n int64) string { return MergeRefPrefix + strconv.FormatInt(n, 10) }

// MergeGitOK refuses a git older than MergeGit, with the sentence a request is refused with.
func (s *Store) MergeGitOK(ctx context.Context) error {
	var line string
	var err error
	if s.GitVersion != nil {
		line, err = s.GitVersion(ctx)
	} else {
		line, err = s.h.runner().Git(ctx, "", "--version")
	}
	if err != nil {
		return err
	}
	m := verRe.FindStringSubmatch(line)
	if m == nil {
		return refuse("the hub cannot read its git version from %q", strings.TrimSpace(line))
	}
	maj, _ := strconv.Atoi(m[1])
	min, _ := strconv.Atoi(m[2])
	if maj < 2 || (maj == 2 && min < 38) {
		return refuse("the hub's git is %s.%s and the merge queue needs %s or later. update git on the hub", m[1], m[2], MergeGit)
	}
	return nil
}

// MergeCandidate merges branch, which has to be at sha, onto claude/main of the repository `name` for request n. A
// branch claude/main already descends from is its own candidate. A conflict answers the paths and no candidate.
func (s *Store) MergeCandidate(ctx context.Context, name, branch, sha string, n int64, msg string) (MergeResult, error) {
	var r MergeResult
	if why := CheckPushBranch(branch); why != "" {
		return r, refuse("%s", why)
	}
	if err := s.MergeGitOK(ctx); err != nil {
		return r, err
	}
	dir, err := s.bareFor(name)
	if err != nil {
		return r, err
	}
	if dir == "" {
		return r, refuse("the hub holds no repository %s", name)
	}
	ctx, cancel := context.WithTimeout(ctx, CommandBound)
	defer cancel()
	tip, ok, err := s.tipOf(ctx, dir, branch)
	if err != nil {
		return r, err
	}
	if !ok {
		return r, refuse("the hub has no branch %s. push it first: git push hub %s", branch, branch)
	}
	if sha != "" && !strings.HasPrefix(tip, strings.ToLower(sha)) {
		return r, refuse("the hub's %s is at %s, not the %s asked to merge. push again, then ask again", branch, short(tip), short(sha))
	}
	base, ok, err := s.tipOf(ctx, dir, IntegrationBranch)
	if err != nil {
		return r, err
	}
	if !ok {
		return r, refuse("the hub's %s has no %s to merge onto", name, IntegrationBranch)
	}
	r.Base, r.Tip = base, tip
	if in, err := s.isAncestor(ctx, dir, tip, base); err != nil {
		return r, err
	} else if in {
		r.Already = true
		return r, nil
	}
	cand := tip
	if ff, err := s.isAncestor(ctx, dir, base, tip); err != nil {
		return r, err
	} else if !ff {
		out, err := s.git(ctx, dir, "merge-tree", "--write-tree", "--name-only", "--no-messages", base, tip)
		lines := strings.Split(strings.TrimSpace(out), "\n")
		switch {
		case err != nil && exitCode(err) == 1:
			for _, l := range lines[1:] {
				if l = strings.TrimSpace(l); l != "" {
					r.Conflicts = append(r.Conflicts, l)
				}
			}
			if len(r.Conflicts) == 0 {
				r.Conflicts = []string{"(git named no path)"}
			}
			return r, nil
		case err != nil:
			return r, err
		}
		tree := strings.TrimSpace(lines[0])
		if !ValidSHA(tree) {
			return r, fmt.Errorf("merge-tree answered %q, not a tree", short(tree))
		}
		env := append(s.env(), "GIT_AUTHOR_NAME="+mergeName, "GIT_AUTHOR_EMAIL="+mergeEmail,
			"GIT_COMMITTER_NAME="+mergeName, "GIT_COMMITTER_EMAIL="+mergeEmail)
		out, err = s.h.runner().GitEnv(ctx, dir, env, "commit-tree", tree, "-p", base, "-p", tip, "-m", msg)
		if err != nil {
			return r, err
		}
		cand = strings.TrimSpace(out)
		if !ValidSHA(cand) {
			return r, fmt.Errorf("commit-tree answered %q, not a commit", short(cand))
		}
	}
	if _, err := s.git(ctx, dir, "update-ref", "-m", fmt.Sprintf("merge queue %d", n), MergeRef(n), cand); err != nil {
		return r, err
	}
	r.Candidate = cand
	return r, nil
}

// MergeLand moves claude/main from base to candidate, which has to have base under it. A claude/main that is not at
// base any more is refused with ErrConflict, and the caller merges again.
func (s *Store) MergeLand(ctx context.Context, name string, n int64, base, candidate string) error {
	ref, err := ParseName(name)
	if err != nil {
		return err
	}
	dir, err := s.bareFor(name)
	if err != nil {
		return err
	}
	if dir == "" {
		return refuse("the hub holds no repository %s", name)
	}
	if !ValidSHA(base) || !ValidSHA(candidate) {
		return refuse("a base and a candidate are full shas")
	}
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	now, _, err := s.tipOf(ctx, dir, IntegrationBranch)
	if err != nil {
		return err
	}
	if now != base {
		return conflict("claude/main moved from %s to %s while request %d was tested", short(base), short(now), n)
	}
	if ok, err := s.isAncestor(ctx, dir, base, candidate); err != nil {
		return err
	} else if !ok {
		return refuse("%s does not have claude/main under it", short(candidate))
	}
	_, err = s.git(ctx, dir, "update-ref", "-m", fmt.Sprintf("merge queue %d", n), ClaudeMainRef, candidate, base)
	return err
}

// MergeDrop removes request n's candidate ref. One that is not there is not an error.
func (s *Store) MergeDrop(ctx context.Context, name string, n int64) error {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	_, err = s.git(ctx, dir, "update-ref", "-d", MergeRef(n))
	return err
}

// DeleteLanded deletes branch from the store when it is still at sha and claude/main has it, under the push lock, so
// a push that moved it since is kept. It answers whether it deleted.
func (s *Store) DeleteLanded(ctx context.Context, name, branch, sha string) (bool, error) {
	if why := CheckPushBranch(branch); why != "" || branch == IntegrationBranch || branch == "main" {
		return false, refuse("the queue deletes a landed claude/* branch and nothing else")
	}
	ref, err := ParseName(name)
	if err != nil {
		return false, err
	}
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return false, err
	}
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	tip, ok, err := s.tipOf(ctx, dir, branch)
	if err != nil || !ok || !strings.HasPrefix(tip, sha) {
		return false, err
	}
	if in, err := s.isAncestor(ctx, dir, tip, ClaudeMainRef); err != nil || !in {
		return false, err
	}
	if _, err := s.git(ctx, dir, "update-ref", "-d", headsPrefix+branch, tip); err != nil {
		return false, err
	}
	return true, nil
}
