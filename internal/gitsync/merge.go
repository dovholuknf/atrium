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

// MinSHAPrefix is the shortest sha prefix the queue takes for a commit.
const MinSHAPrefix = 7

// MergeGit is the oldest git with `merge-tree --write-tree`.
const MergeGit = "2.38"

// The identity of a merge commit the queue makes. A land is one commit by the operator, so main never carries it.
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
	if sha != "" && (len(sha) < MinSHAPrefix || !strings.HasPrefix(tip, strings.ToLower(sha))) {
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
// base any more is refused with ErrConflict, and the caller merges again. One already at candidate is landed, so a
// verdict sent again after the store failed to record it lands it once.
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
	if now == candidate {
		return nil
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
	if len(sha) < MinSHAPrefix {
		return false, refuse("a landed branch is named with its sha")
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

// LandRefPrefix keeps each land's commit, so a main that is a land is told from one a person pushed.
const LandRefPrefix = "refs/atrium/lands/"

// LandAuthor is who a land commit is by: the operator, from their own git config.
type LandAuthor struct{ Name, Email string }

// OperatorAuthor reads user.name and user.email from the operator's global git config.
func OperatorAuthor(ctx context.Context) (LandAuthor, error) {
	cfg := func(k string) string {
		out, _ := Default.Git(ctx, "", "config", "--global", "--get", k)
		return strings.TrimSpace(out)
	}
	a := LandAuthor{Name: cfg("user.name"), Email: cfg("user.email")}
	if a.Name == "" || a.Email == "" {
		return a, refuse("set git config --global user.name and user.email on the hub's machine")
	}
	return a, nil
}

// LandCut is what one land made: Commit, with claude/main's tree and main for its parent, now main. Moved is false
// when main already had claude/main's tree, and Commit is then main as it was.
type LandCut struct {
	Commit       string `json:"commit"`
	MainBefore   string `json:"main_before"`
	ClaudeBefore string `json:"claude_before"`
	Moved        bool   `json:"moved"`
}

// MergeLandCut makes land n under the push lock: one commit whose tree is claude/main's and whose parent is main,
// written to main and to LandRefPrefix<n> in one transaction. claude/main is not moved, so no branch cut from it
// loses its merge base. main has to be under claude/main, or be the last land's commit.
func (s *Store) MergeLandCut(ctx context.Context, name string, n int64, by LandAuthor, msg string) (LandCut, error) {
	var c LandCut
	ref, err := ParseName(name)
	if err != nil {
		return c, err
	}
	dir, err := s.bareFor(name)
	if err != nil {
		return c, err
	}
	if dir == "" {
		return c, refuse("the hub holds no repository %s", name)
	}
	l := s.h.lock("store:" + strings.ToLower(ref.Name()))
	l.Lock()
	defer l.Unlock()
	if ml := s.mirrorLock(dir); ml != nil {
		ml.Lock()
		defer ml.Unlock()
	}
	if markerKind(dir) == KindAdopted {
		return c, refuse("%s", adoptedSentence(name))
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	for branch, into := range map[string]*string{IntegrationBranch: &c.ClaudeBefore, "main": &c.MainBefore} {
		tip, ok, err := s.tipOf(ctx, dir, branch)
		if err != nil {
			return c, err
		}
		if !ok {
			return c, refuse("the repository has no %s, so there is nothing to land", branch)
		}
		*into = tip
	}
	under, err := s.isAncestor(ctx, dir, c.MainBefore, c.ClaudeBefore)
	if err != nil {
		return c, err
	}
	if !under {
		out, err := s.git(ctx, dir, "for-each-ref", "--count=1", "--format=%(refname)", "--points-at", c.MainBefore, LandRefPrefix)
		if err != nil {
			return c, err
		}
		if strings.TrimSpace(out) == "" {
			return c, refuse("main (%s) is neither under claude/main nor a land, so a land would undo what main has", short(c.MainBefore))
		}
	}
	trees, err := s.git(ctx, dir, "rev-parse", c.ClaudeBefore+"^{tree}", c.MainBefore+"^{tree}")
	if err != nil {
		return c, err
	}
	tt := strings.Fields(trees)
	if len(tt) != 2 || !ValidSHA(tt[0]) {
		return c, fmt.Errorf("rev-parse answered %q, not two trees", short(trees))
	}
	c.Commit = c.MainBefore
	if tt[0] != tt[1] {
		env := append(s.env(), "GIT_AUTHOR_NAME="+by.Name, "GIT_AUTHOR_EMAIL="+by.Email,
			"GIT_COMMITTER_NAME="+by.Name, "GIT_COMMITTER_EMAIL="+by.Email)
		out, err := s.h.runner().GitEnv(ctx, dir, env, "commit-tree", "--no-gpg-sign", tt[0], "-p", c.MainBefore, "-m", msg)
		if err != nil {
			return c, err
		}
		if c.Commit = strings.TrimSpace(out); !ValidSHA(c.Commit) {
			return c, fmt.Errorf("commit-tree answered %q, not a commit", short(c.Commit))
		}
		c.Moved = true
	}
	tx := fmt.Sprintf("create %s%d %s\n", LandRefPrefix, n, c.Commit)
	if c.Moved {
		tx += fmt.Sprintf("update %s %s %s\n", MainRef, c.Commit, c.MainBefore)
	}
	_, err = s.h.runner().gitWith(ctx, dir, s.env(), []byte(tx), 0, "update-ref", "-m", fmt.Sprintf("land %d", n), "--stdin")
	return c, err
}

// OnMain reports whether sha is on main of the repository `name`.
func (s *Store) OnMain(ctx context.Context, name, sha string) (bool, error) {
	dir, err := s.bareFor(name)
	if err != nil || dir == "" {
		return false, err
	}
	ctx, cancel := context.WithTimeout(ctx, readBound)
	defer cancel()
	return s.isAncestor(ctx, dir, sha, MainRef)
}
