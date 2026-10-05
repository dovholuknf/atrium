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
	// Host is the forge host the hub placed the PR by. It finds the provider when the name in the path is not one this
	// room has.
	Host   string `json:"host"`
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
	var req prWorktreeRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&req); err != nil {
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	p, err := s.st.Provider(r.PathValue("name"))
	if err != nil {
		// THE HUB PLACES BY HOST, so a room whose provider for that host has another name still takes the PR.
		if p = s.providerByHost(req.Host); p == nil {
			writeErr(w, http.StatusNotFound, errNoProvider)
			return
		}
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
		if s.ForgeFailed != nil {
			s.ForgeFailed(f.Kind(), err)
		}
		writeErr(w, http.StatusBadRequest, err)
		return
	}
	if s.ForgeWorked != nil {
		s.ForgeWorked(f.Kind(), host)
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
		helper := credentialHelper(f.Kind(), p.ForgeCmd)
		fetch = func(ctx context.Context, dir string, spec forge.FetchSpec, dst string) error {
			if spec.Hub == "" {
				return fetchPRHead(ctx, dir, spec, dst, helper)
			}
			// THE HEAD IS IN THE HUB'S STORE: the room reads it through its link, never from the forge.
			if s.HubSource == nil {
				return errors.New("this room has no way to read the hub's store")
			}
			url, done, err := s.HubSource(ctx, spec.Hub)
			if err != nil {
				return err
			}
			defer done()
			spec.Remote = url
			return fetchPRHead(ctx, dir, spec, dst, "")
		}
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

// credentialHelper is the git helper line of a forge's own CLI, or "" for a forge with none. The command name is the
// provider's forge_cmd, else the kind's default.
func credentialHelper(kind, cmd string) string {
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		switch kind {
		case forge.GitHub:
			cmd = "gh"
		default:
			return ""
		}
	}
	return "!" + cmd + " auth git-credential"
}

// fetchArgs is the git argv for one PR head fetch. A helper is set for this one command only: the configured ones
// are reset, then the forge CLI's own is the only one asked. It answers from the CLI's login, so no token is read,
// stored or put in the argv.
func fetchArgs(spec forge.FetchSpec, dst, helper string) []string {
	args := []string{"-c", "protocol.allow=never", "-c", "protocol.https.allow=always"}
	if spec.Hub != "" {
		// The hub's store, through a loopback on this machine, which speaks http. No helper: it needs no credential.
		args, helper = []string{"-c", "protocol.allow=never", "-c", "protocol.http.allow=always"}, ""
	}
	if helper != "" {
		args = append(args, "-c", "credential.helper=", "-c", "credential.helper="+helper)
	}
	return append(args, "fetch", "--no-tags", "--", spec.Remote, "+"+spec.Refspec+":"+dst)
}

// fetchPRHead fetches the head into dst over https only, so the URL the forge gave cannot be another scheme. A
// private repo is read through helper, see fetchArgs. What is logged is git's own output, which holds no token.
func fetchPRHead(ctx context.Context, dir string, spec forge.FetchSpec, dst, helper string) error {
	cmd := exec.CommandContext(ctx, "git", fetchArgs(spec, dst, helper)...)
	cmd.Dir = dir
	allow := "https"
	if spec.Hub != "" {
		allow = "http"
	}
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_ALLOW_PROTOCOL="+allow)
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(tailLines(string(out))))
	}
	return nil
}

// providerByHost is the enabled provider with worktrees on for a forge host, or nil.
func (s *Server) providerByHost(host string) *store.Provider {
	host = strings.ToLower(strings.TrimSpace(host))
	if host == "" {
		return nil
	}
	rows, err := s.st.Providers()
	if err != nil {
		return nil
	}
	for _, p := range rows {
		h := strings.ToLower(strings.TrimSpace(p.Host))
		if h == "" {
			h = "github.com"
		}
		if p.Enabled && p.Worktrees && h == host {
			return p
		}
	}
	return nil
}
