package daemon

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/rules"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
	"github.com/dovholuknf/atrium/internal/worktrees"
)

// POST /v1/worktrees/audit: one line per worktree of a repository, and whether it could go. The git half is
// internal/worktrees. This is the half only the room knows: which worktrees atrium made, and which a card is using.
// See docs/runtime/worktrees-tool-design.md. The room runs every git command and the hub runs none.

type worktreesAuditRequest struct {
	Repo   string   `json:"repo"`
	Prefix string   `json:"prefix"`
	Paths  []string `json:"paths"`
	// Caller is the wire name of the session that asked, whose directory is the repo when none is given.
	Caller string `json:"caller"`
}

func (d *Daemon) handleWorktreesAudit(w http.ResponseWriter, r *http.Request) {
	var in worktreesAuditRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil && !errors.Is(err, io.EOF) {
		worktreesFail(w, http.StatusBadRequest, "the body is not json: "+err.Error())
		return
	}
	repo, err := d.worktreesRepo(strings.TrimSpace(in.Repo), strings.TrimSpace(in.Caller))
	if err != nil {
		worktreesFail(w, http.StatusBadRequest, err.Error())
		return
	}
	res, err := worktrees.Audit(r.Context(), worktrees.Input{Repo: repo, Prefix: in.Prefix, Paths: in.Paths,
		Judge: d.worktreeJudge()})
	if err != nil {
		var ref *worktrees.Refusal
		if errors.As(err, &ref) {
			worktreesFail(w, http.StatusBadRequest, ref.Why)
			return
		}
		worktreesFail(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}

func worktreesFail(w http.ResponseWriter, code int, why string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": why})
}

// worktreesRepo is a directory inside the repository: the one named, the clone of host/org/repo this room holds, or
// the asking session's own. It never clones: a repo the room has no clone of is refused.
func (d *Daemon) worktreesRepo(repo, caller string) (string, error) {
	if repo == "" {
		t, err := d.st.GetByWireName(caller)
		if caller == "" || err != nil || t == nil || strings.TrimSpace(t.Worktree) == "" {
			return "", errors.New("no repo was given and the caller has no directory to take it from. " +
				"give repo as a path inside a checkout, or host/org/repo")
		}
		repo = t.Worktree
	}
	if fi, err := os.Stat(filepath.FromSlash(repo)); err == nil && fi.IsDir() {
		return repo, nil
	}
	if !rules.RepoSlug(repo) {
		return "", errors.New("repo is a path inside a checkout that exists, or host/org/repo, like github.com/openziti/ziti")
	}
	if p := d.checkoutOf(repo); p != "" {
		return p, nil
	}
	return "", errors.New("this room has no clone of " + repo)
}

// checkoutOf is where this room holds host/org/repo, or empty: a provider's folder first, then the scm clone.
func (d *Daemon) checkoutOf(slug string) string {
	parts := strings.Split(slug, "/")
	if len(parts) != 3 {
		return ""
	}
	host, org, name := strings.ToLower(parts[0]), parts[1], parts[2]
	if rows, err := d.st.Providers(); err == nil {
		for _, p := range rows {
			h := strings.ToLower(strings.TrimSpace(p.Host))
			if h == "" {
				h = "github.com"
			}
			if h != host {
				continue
			}
			if path := store.ProviderPath(p.Root, org, name); path != "" && hasDotGit(path) {
				return path
			}
		}
	}
	ref, err := gitsync.ParseURL("https://" + slug)
	if err != nil {
		return ""
	}
	if _, dest, err := d.scm(nil).ClonePath(ref); err == nil && hasDotGit(dest) {
		return filepath.ToSlash(dest)
	}
	return ""
}

// worktreeJudge is the room's say on a worktree path, read once for the call.
//
// A path is atrium's when an inventory row names it, or a card tagged origin:agent and atrium:subagent works in it.
// Everything else is not, whoever asks: the operator's worktrees, an orchestrator's, and a worker's whose card the sweep
// deleted. Then a card in use, and the sentence reclaimBlock gives, follow as reasons.
func (d *Daemon) worktreeJudge() func(string) worktrees.Judgment {
	tasks, _ := d.st.List()
	rows, _ := d.st.LiveResources()
	return func(path string) worktrees.Judgment {
		j := worktrees.Judgment{}
		var cards []*store.Task
		have := map[string]bool{}
		add := func(t *store.Task) {
			if t != nil && !have[t.ID] {
				have[t.ID] = true
				cards = append(cards, t)
			}
		}
		for _, t := range tasks {
			if worktrees.SamePath(t.Worktree, path) {
				add(t)
				if small.HasTag(t.Tags, OriginAgentTag) && small.HasTag(t.Tags, SubagentTag) {
					j.Atriums = true
				}
			}
		}
		for _, r := range rows {
			if r.Kind == store.ResWorktree && worktrees.SamePath(r.Ref, path) {
				j.Atriums = true
				if t, err := d.st.Get(r.Card); err == nil {
					add(t)
				}
			}
		}
		seen := map[string]bool{}
		for _, t := range cards {
			why := ""
			if d.worktreeInUse(t) {
				why = "in use: " + t.DisplayTitle()
			} else {
				why = d.reclaimBlock(t)
			}
			if why != "" && !seen[why] {
				seen[why] = true
				j.Later = append(j.Later, why)
			}
		}
		return j
	}
}

// worktreeInUse is a live runner in the card's worktree, or a new context in progress for it.
func (d *Daemon) worktreeInUse(t *store.Task) bool {
	return d.sup.Get(t.ID) != nil || (t.PID > 0 && !finishedStatus(t.Status)) || d.nctx.holding(t.ID)
}
