package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/gitexec"
	"github.com/dovholuknf/atrium/internal/base/rules"
)

// MoveWorktree is room B's half of carrying a card's branch on a move: the repo is found or cloned as POST /v1/new
// does, the bundle is fetched into refs/heads/<branch> fast-forward only, and the worktree is made at this room's own
// layout. With no bundle the branch is made at base, a commit origin has. A branch that already has a worktree here
// answers that worktree. branchCreated says the branch was not here before, so an undo deletes it.
func (s *Server) MoveWorktree(ctx context.Context, slug, branch, base string, bundle []byte) (path string, created, branchCreated bool, status int, err error) {
	slug = strings.Trim(strings.TrimSpace(slug), "/")
	if !rules.RepoSlug(slug) {
		return "", false, false, http.StatusBadRequest, errors.New("repo is host/org/repo")
	}
	parts := strings.Split(slug, "/")
	host, org, repo := strings.ToLower(parts[0]), parts[1], parts[2]
	p := s.providerByHost(host)
	repoPath, _, _, status, err := s.repoCheckout(ctx, p, host, org, repo)
	if err != nil {
		return "", false, false, status, err
	}
	if err := checkBranchName(ctx, branch); err != nil {
		return "", false, false, http.StatusBadRequest, err
	}
	_, _ = gitIn(ctx, repoPath, gitexec.Fetch{Remote: "origin", Quiet: true})
	branchCreated = !branchExists(ctx, repoPath, branch)
	if len(bundle) > 0 {
		f, err := os.CreateTemp("", "atrium-move-*.bundle")
		if err != nil {
			return "", false, false, http.StatusInternalServerError, err
		}
		defer os.Remove(f.Name())
		_, werr := f.Write(bundle)
		if cerr := f.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			return "", false, false, http.StatusInternalServerError, werr
		}
		if out, err := gitIn(ctx, repoPath, gitexec.BundleVerify{File: filepath.ToSlash(f.Name())}); err != nil {
			return "", false, false, http.StatusConflict, errors.New("this clone cannot take the bundle: " + strings.TrimSpace(out))
		}
		if _, err := gitIn(ctx, repoPath, gitexec.Fetch{Remote: filepath.ToSlash(f.Name()), Quiet: true,
			Refspecs: []string{"refs/heads/" + branch + ":refs/heads/" + branch}}); err != nil {
			return "", false, false, http.StatusConflict, err
		}
	}
	if len(bundle) == 0 && base != "" && !branchExists(ctx, repoPath, branch) {
		if _, err := gitIn(ctx, repoPath, gitexec.BranchCreate{Branch: branch, Start: base}); err != nil {
			return "", false, false, http.StatusConflict, err
		}
	}
	res, status, err := s.branchWorktreeFrom(ctx, p, host, org, repo, branch, false, branchOpts{})
	if err != nil {
		return "", false, false, status, err
	}
	return res.Path, !res.Existed, branchCreated, http.StatusOK, nil
}

// RemoveMoveWorktree undoes a MoveWorktree that made the worktree. A branch the move made goes too, so a retry is not
// refused for a stale tip. Any other branch stays: it is the card's work.
func (s *Server) RemoveMoveWorktree(ctx context.Context, path, branch string) error {
	common, err := gitIn(ctx, path, gitexec.RevParseGitDirs{Absolute: true, Common: true})
	if err != nil {
		return err
	}
	repoPath := filepath.Dir(strings.TrimSpace(common))
	if _, err = gitIn(ctx, repoPath, gitexec.WorktreeRemove{Path: filepath.FromSlash(path), Force: 1}); err != nil || branch == "" {
		return err
	}
	if err := checkBranchName(ctx, branch); err != nil {
		return err
	}
	_, err = gitIn(ctx, repoPath, gitexec.BranchDelete{Branch: branch})
	return err
}
