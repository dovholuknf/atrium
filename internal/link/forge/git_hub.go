package forge

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
)

// The hub's git side, as the board API and the control tools see it. See git.go for the
// transport and internal/gitsync for what actually runs.
//
// NO ROUTE AND NO TOOL HERE TAKES A PATH, A URL, A REFSPEC OR A BRANCH, WITH ONE EXCEPTION:
// POST /_hub/git/init takes a forge URL, from the operator on the hub's machine only. The URL
// is parsed into host, owner and repository and refused if it carries a credential, and only
// those three parts are kept (internal/gitsync/store_name.go). Everything else is a room, a
// repository name from `git_repos`, and whether to init, and the rest is the operator's setting.
// GET /_hub/git/url and atrium_git_url take a repository and a branch to LOOK UP, and give neither to git: the repository
// is found in the list the hub knows, and the branch is compared with the names that were listed (git_url.go).

// SetGit wires the hub's git side into the board API. Nil leaves /_hub/git answering 404.
func (p *Service) SetGit(g *gitsync.Hub) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gitHub = g
}

func (p *Service) Git() *gitsync.Hub {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.gitHub
}

// GitSettings is what /_hub/git/settings needs of the hub's store. hubstore.Store has all four.
type GitSettings interface {
	GitStorePath(hubDir string) (string, error)
	SetGitStore(dir string) error
	GitCreateOnPush() (bool, error)
	SetGitCreateOnPush(on bool) error
}

// StashRetentionSettings is the retention of a close's stash, which hubstore.Store has too. GitSettings does not ask for
// it so a stand-in that predates it still serves the rest.
type StashRetentionSettings interface {
	StashRetentionDays() (int, error)
	SetStashRetentionDays(days int) error
}

// SetGitSettings wires the git.store and git.create_on_push settings. Without it /_hub/git/settings
// answers 404.
func (p *Service) SetGitSettings(s GitSettings, hubDir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gitSettings, p.gitDir = s, hubDir
}

// serveGit answers /_hub/git/{sync,collect,status,init,release,settings,repos,branches,url}. Loopback only, like the
// control server, for everything but the list of repos: these start work on a room or on the
// hub's disk, and an overlay is not an auth layer.
func (p *Service) ServeGit(w http.ResponseWriter, r *http.Request, sub string) {
	g := p.Git()
	if g == nil {
		http.NotFound(w, r)
		return
	}
	fail := func(code int, msg string) {
		w.WriteHeader(code)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
	}
	// OPEN LIKE GET /_hub/growls: the board lists the hub's repositories, and the answer names no
	// path on the hub's disk.
	if sub == "git/repos" {
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "that has to be a GET")
			return
		}
		// FROM THE SNAPSHOT, never from git: see gitsync/viewcache.go.
		snap := g.Store().Snapshot()
		ans := map[string]any{"repos": snap.Repos, "building": snap.Building}
		if !snap.Computed.IsZero() {
			ans["computed"] = snap.Computed.UTC().Format(time.RFC3339)
			ans["age_seconds"] = int(time.Since(snap.Computed).Seconds())
		}
		_ = json.NewEncoder(w).Encode(ans)
		return
	}
	// OPEN LIKE THE LIST OF REPOSITORIES, and from a snapshot like it: see gitsync/branchstatus.go. Branch names are
	// checked before they are drawn, and no path on the hub's disk is named.
	if sub == "git/branches" {
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "that has to be a GET")
			return
		}
		q := r.URL.Query()
		lim, _ := strconv.Atoi(q.Get("limit"))
		repo := ""
		if q.Get("repo") != "" {
			ref, err := gitsync.ParseName(q.Get("repo"))
			if err != nil {
				fail(http.StatusBadRequest, "repo is host/owner/repo, like github/o/r")
				return
			}
			repo = ref.Name()
		}
		for _, st := range q["status"] {
			switch st {
			case gitsync.BranchLanded, gitsync.BranchOpen, gitsync.BranchOrphaned, gitsync.BranchRoomOnly:
			default:
				fail(http.StatusBadRequest, "status is landed, open, orphaned or room-only")
				return
			}
		}
		if k := q.Get("kind"); k != "" && k != gitsync.KindCode && k != gitsync.KindDocs {
			fail(http.StatusBadRequest, "kind is code or docs")
			return
		}
		v, err := g.Store().BranchesSnapshot(repo, gitsync.BranchFilter{Statuses: q["status"], Kind: q.Get("kind"), Limit: lim})
		if err != nil {
			fail(http.StatusServiceUnavailable, "the hub could not read its store just now")
			return
		}
		_ = json.NewEncoder(w).Encode(v)
		return
	}
	// THE LOOKUP has the reaches of a fetch, not of a sync: it starts no work and names no path. See git_url.go.
	if sub == "git/url" {
		p.ServeGitURL(w, r, g, fail)
		return
	}
	if !edge.LocalOperator(r) {
		fail(http.StatusForbidden, "git sync is started only from the machine the hub runs on"+edge.ProxyNote(r))
		return
	}
	switch sub {
	case "git/init":
		p.serveGitInit(w, r, g, fail)
		return
	case "git/settings":
		p.serveGitSettings(w, r, fail)
		return
	case "git/release":
		p.serveGitRelease(w, r, g, fail)
		return
	}
	if sub == "git/status" {
		if r.Method != http.MethodGet {
			fail(http.StatusMethodNotAllowed, "that has to be a GET")
			return
		}
		_ = json.NewEncoder(w).Encode(g.Status())
		return
	}
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "that has to be a POST")
		return
	}
	var in struct {
		Room string `json:"room"`
		Name string `json:"name"`
		Init bool   `json:"init"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	if strings.TrimSpace(in.Room) == "" {
		fail(http.StatusBadRequest, "say which room")
		return
	}
	switch sub {
	case "git/sync":
		res, err := g.Sync(r.Context(), in.Room, in.Name, in.Init)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		p.RecordAudit(in.Room, "git-sync-asked", fmt.Sprintf("%s init=%v", in.Name, in.Init))
		_ = json.NewEncoder(w).Encode(map[string]any{"results": res})
	case "git/collect":
		res, err := g.Collect(r.Context(), in.Room)
		if err != nil {
			fail(http.StatusConflict, err.Error())
			return
		}
		p.RecordAudit(in.Room, "git-collect-asked", "")
		_ = json.NewEncoder(w).Encode(res)
	default:
		http.NotFound(w, r)
	}
}

// serveGitInit answers POST /_hub/git/init {"url": ...} with {repo, created, seeded, main, note}.
// The caller is already known to be the operator on this machine.
func (p *Service) serveGitInit(w http.ResponseWriter, r *http.Request, g *gitsync.Hub, fail func(int, string)) {
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "that has to be a POST")
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	res, err := g.Store().Init(r.Context(), in.URL)
	switch {
	case errors.Is(err, gitsync.ErrRefused):
		fail(http.StatusBadRequest, err.Error())
	case errors.Is(err, gitsync.ErrConflict):
		fail(http.StatusConflict, err.Error())
	case err != nil:
		fail(http.StatusInternalServerError, err.Error())
	default:
		_ = json.NewEncoder(w).Encode(res)
	}
}

// serveGitRelease answers POST /_hub/git/release {"repo": ..., "branch": ...} with {"note": ...}: the operator lets
// go of a branch's owner. The caller is already known to be the operator on this machine.
func (p *Service) serveGitRelease(w http.ResponseWriter, r *http.Request, g *gitsync.Hub, fail func(int, string)) {
	if r.Method != http.MethodPost {
		fail(http.StatusMethodNotAllowed, "that has to be a POST")
		return
	}
	var in struct {
		Repo   string `json:"repo"`
		Branch string `json:"branch"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
		fail(http.StatusBadRequest, "could not read that: "+err.Error())
		return
	}
	note, err := g.ReleaseBranch(r.Context(), in.Repo, in.Branch)
	switch {
	case errors.Is(err, gitsync.ErrRefused):
		fail(http.StatusBadRequest, err.Error())
	case err != nil:
		fail(http.StatusInternalServerError, err.Error())
	default:
		_ = json.NewEncoder(w).Encode(map[string]string{"note": note})
	}
}

// serveGitSettings answers GET and PUT /_hub/git/settings: {"store": "<dir>", "create_on_push": false}.
// A PUT sets only what it names, and `store` empty puts the directory back to the default. BOTH
// ARE FOR THE MACHINE THE HUB RUNS ON, the GET too, because `store` is a path on the hub's disk.
func (p *Service) serveGitSettings(w http.ResponseWriter, r *http.Request, fail func(int, string)) {
	p.mu.Lock()
	st, dir := p.gitSettings, p.gitDir
	p.mu.Unlock()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	switch r.Method {
	case http.MethodGet:
	case http.MethodPut:
		var in struct {
			Store        *string `json:"store"`
			CreateOnPush *bool   `json:"create_on_push"`
			StashDays    *int    `json:"stash_retention_days"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		if in.Store == nil && in.CreateOnPush == nil && in.StashDays == nil {
			fail(http.StatusBadRequest, "say store, create_on_push or stash_retention_days")
			return
		}
		if in.StashDays != nil {
			sr, ok := st.(StashRetentionSettings)
			if !ok {
				fail(http.StatusNotImplemented, "this hub cannot set the stash retention")
				return
			}
			if err := sr.SetStashRetentionDays(*in.StashDays); err != nil {
				fail(http.StatusBadRequest, err.Error())
				return
			}
		}
		if in.Store != nil {
			if err := st.SetGitStore(*in.Store); err != nil {
				fail(http.StatusBadRequest, err.Error())
				return
			}
		}
		if in.CreateOnPush != nil {
			if err := st.SetGitCreateOnPush(*in.CreateOnPush); err != nil {
				fail(http.StatusInternalServerError, "could not save that: "+err.Error())
				return
			}
		}
		p.RecordAudit("", "git-settings-set", fmt.Sprintf("store=%v create_on_push=%v", in.Store != nil, in.CreateOnPush != nil))
	default:
		fail(http.StatusMethodNotAllowed, "that has to be a GET or a PUT")
		return
	}
	path, err := st.GitStorePath(dir)
	if err != nil {
		fail(http.StatusInternalServerError, "could not read the settings: "+err.Error())
		return
	}
	on, err := st.GitCreateOnPush()
	if err != nil {
		fail(http.StatusInternalServerError, "could not read the settings: "+err.Error())
		return
	}
	out := map[string]any{"store": path, "create_on_push": on}
	if sr, ok := st.(StashRetentionSettings); ok {
		if n, err := sr.StashRetentionDays(); err == nil {
			out["stash_retention_days"] = n
		}
	}
	_ = json.NewEncoder(w).Encode(out)
}
