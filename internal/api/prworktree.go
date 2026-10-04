package api

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/store"
)

// prWorktreeRef is where the head of a PR is fetched to, whatever branch it ends up on.
func prWorktreeRef(n int) string { return "refs/atrium/pr/" + strconv.Itoa(n) }

type prWorktreeRequest struct {
	Org    string `json:"org"`
	Repo   string `json:"repo"`
	Number int    `json:"number"`
}

// makePRWorktree makes a worktree for a pull request at `<worktree_root>/<org>/<repo>/<branch>`.
//
//   - The head ref and fork-ness come from the forge (View, FetchSpec), never from gh directly. A forge that
//     is missing or logged out answers its own sentence, before anything is cloned.
//   - A same-repo PR is checked out on its real head branch, a fork PR on `pr-<N>`.
//   - A worktree already on that branch is the answer (`existed: true`).
//   - A provider with no checkout of the repo does not refuse. The code comes through the scm clone path, which
//     clones with its `hub` remote and a guarded `origin`, and a repo it cannot clone answers gitsync.CloneFailed.
func (s *Server) makePRWorktree(w http.ResponseWriter, r *http.Request) {
	p, err := s.st.Provider(r.PathValue("name"))
	if err != nil {
		writeErr(w, http.StatusNotFound, errNoProvider)
		return
	}
	var req prWorktreeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if !p.Worktrees {
		writeErr(w, http.StatusBadRequest, errors.New(
			"worktree support is off for "+p.Name+". turn it on and give it a folder first"))
		return
	}
	if req.Number <= 0 || strings.TrimSpace(req.Org) == "" || strings.TrimSpace(req.Repo) == "" {
		writeErr(w, http.StatusBadRequest, errors.New("which pull request: org, repo and number"))
		return
	}
	host := p.Host
	if host == "" {
		host = "github.com"
	}
	if s.PRForge == nil {
		writeErr(w, http.StatusServiceUnavailable, errors.New("this room has no forge to ask about pull requests"))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), makeWorktreeDeadline)
	defer cancel()

	f, err := s.PRForge(host)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	ref := forge.Ref{Host: host, Org: req.Org, Repo: req.Repo, Number: req.Number}
	pr, err := f.View(ctx, ref)
	if err != nil {
		// An AccessError and a no_forge carry their own sentence, unchanged.
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	branch := pr.HeadRef
	if pr.FromFork || strings.TrimSpace(branch) == "" {
		branch = "pr-" + strconv.Itoa(req.Number)
	}

	repoPath := store.ProviderPath(p.Root, req.Org, req.Repo)
	if repoPath == "" || !isCheckout(filepath.FromSlash(repoPath)) {
		if s.SCMClone == nil {
			writeErr(w, http.StatusServiceUnavailable, errors.New("this room cannot clone"))
			return
		}
		res, err := s.SCMClone(ctx, "https://"+host+"/"+req.Org+"/"+req.Repo)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err)
			return
		}
		repoPath = res.Path
	}

	for _, wt := range worktreesOf(repoPath) {
		if strings.EqualFold(wt.Branch, branch) {
			writeJSON(w, http.StatusOK, map[string]any{"path": wt.Path, "existed": true, "branch": wt.Branch})
			return
		}
	}
	dest := worktreeDest(p.WorktreeRoot, req.Org, req.Repo, branch)
	if dest == "" {
		writeErr(w, http.StatusBadRequest, errors.New("which repository"))
		return
	}
	if _, err := os.Stat(filepath.FromSlash(dest)); err == nil {
		writeErr(w, http.StatusConflict, errors.New(
			dest+" already exists but is not a worktree of that branch. move it aside, or use a different branch name"))
		return
	}

	fetch := s.PRFetch
	if fetch == nil {
		fetch = fetchPRHead
	}
	if err := fetch(ctx, filepath.FromSlash(repoPath), f.FetchSpec(ref), prWorktreeRef(req.Number)); err != nil {
		writeErr(w, http.StatusBadRequest, errors.New("could not fetch the head of pull request "+strconv.Itoa(req.Number)+
			": "+err.Error()))
		return
	}
	if err := os.MkdirAll(filepath.Dir(filepath.FromSlash(dest)), 0o755); err != nil {
		s.fail(w, err)
		return
	}
	// An existing local branch is checked out as it is, so a local commit is not overwritten by a fetch.
	made := !branchExists(ctx, repoPath, branch)
	args := []string{"worktree", "add", filepath.FromSlash(dest), branch}
	if made {
		args = []string{"worktree", "add", "-b", branch, filepath.FromSlash(dest), prWorktreeRef(req.Number)}
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = filepath.FromSlash(repoPath)
	out, err := cmd.CombinedOutput()
	if err != nil {
		writeErr(w, http.StatusBadRequest, errors.New(
			"git could not make that worktree: "+strings.TrimSpace(tailLines(string(out)))))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"path": dest, "existed": false, "branch": branch, "created_branch": made,
	})
}

// fetchPRHead fetches the head into dst over https only, so the URL the forge gave cannot be another scheme.
func fetchPRHead(ctx context.Context, dir string, spec forge.FetchSpec, dst string) error {
	cmd := exec.CommandContext(ctx, "git", "-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"fetch", "--no-tags", "--", spec.Remote, "+"+spec.Refspec+":"+dst)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL=https")
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(tailLines(string(out))))
	}
	return nil
}
