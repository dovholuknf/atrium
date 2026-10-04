package link

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
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

// GitRooms adapts this hub to what internal/gitsync needs of the link.
func (h *Hub) GitRooms() gitsync.Rooms { return hubGitRooms{h} }

type hubGitRooms struct{ h *Hub }

func (g hubGitRooms) Attached() []gitsync.RoomInfo {
	var out []gitsync.RoomInfo
	for _, a := range g.h.Rooms() {
		out = append(out, gitsync.RoomInfo{Name: a.Name, Host: a.Host, Git: a.Git})
	}
	return out
}

func (g hubGitRooms) Transport(room string) http.RoundTripper { return g.h.Transport(room) }

// SetGit wires the hub's git side into the board API. Nil leaves /_hub/git answering 404.
func (p *Proxy) SetGit(g *gitsync.Hub) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gitHub = g
}

func (p *Proxy) git() *gitsync.Hub {
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

// SetGitSettings wires the git.store and git.create_on_push settings. Without it /_hub/git/settings
// answers 404.
func (p *Proxy) SetGitSettings(s GitSettings, hubDir string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.gitSettings, p.gitDir = s, hubDir
}

// serveGit answers /_hub/git/{sync,collect,status,init,release,settings,repos,url}. Loopback only, like the
// control server, for everything but the list of repos: these start work on a room or on the
// hub's disk, and an overlay is not an auth layer.
func (p *Proxy) serveGit(w http.ResponseWriter, r *http.Request, sub string) {
	g := p.git()
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
		repos, err := g.Store().View(r.Context())
		if err != nil {
			fail(http.StatusServiceUnavailable, "could not list the repositories")
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"repos": repos})
		return
	}
	// THE LOOKUP has the reaches of a fetch, not of a sync: it starts no work and names no path. See git_url.go.
	if sub == "git/url" {
		p.serveGitURL(w, r, g, fail)
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
func (p *Proxy) serveGitInit(w http.ResponseWriter, r *http.Request, g *gitsync.Hub, fail func(int, string)) {
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
func (p *Proxy) serveGitRelease(w http.ResponseWriter, r *http.Request, g *gitsync.Hub, fail func(int, string)) {
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
func (p *Proxy) serveGitSettings(w http.ResponseWriter, r *http.Request, fail func(int, string)) {
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
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 4<<10)).Decode(&in); err != nil {
			fail(http.StatusBadRequest, "could not read that: "+err.Error())
			return
		}
		if in.Store == nil && in.CreateOnPush == nil {
			fail(http.StatusBadRequest, "say store, create_on_push, or both")
			return
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
	_ = json.NewEncoder(w).Encode(map[string]any{"store": path, "create_on_push": on})
}

// ── control tools ───────────────────────────────────────

// gitTimeout is how long a control tool waits for a sync or a collect. A first clone over
// an overlay is minutes.
const gitTimeout = gitsync.CommandBound + 2*time.Minute

type gitSyncInput struct {
	Room string `json:"room" jsonschema:"the room to sync, as named on the hub"`
	Name string `json:"name,omitempty" jsonschema:"one repository from the hub's git_repos, or empty for all of them"`
	Init bool   `json:"init,omitempty" jsonschema:"make the clone when the room has none. only ever on a person's or a director's word"`
}

type gitSyncOutput struct {
	Results []gitsync.SyncResult `json:"results"`
}

type gitCollectInput struct {
	Room string `json:"room" jsonschema:"the room to collect claude/* branches from, as named on the hub"`
}

const gitSyncToolDesc = "Ask the hub to bring a room's clone up to date with claude/main.\n\n" +
	"THE HUB DOES THIS ON ITS OWN when claude/main moves, when a room attaches, and every five " +
	"minutes for collecting. Call it to do that now and read the answer.\n\n" +
	"Each repository answers one of: `ok` (fetched, and claude/main and hub-main are at the hub's sha), " +
	"`absent` (no clone there, and `init` was false, so nothing was run), `behind` (fetched, but git " +
	"refused to move a branch: a worktree holds claude/main, or hub-main is checked out with changes. " +
	"`sha` is what was fetched), `failed` (nothing was fetched: `detail` is git's own words), or " +
	"`unsupported` (that room's build predates git sync).\n\n" +
	"`init` makes the clone. Pass it only when the person or director you work for asked for a new " +
	"clone on that room. There is no path, url, branch or refspec to pass: the repositories are the " +
	"hub's `git_repos` and the branch is the integration branch."

const gitCollectToolDesc = "Ask the hub to fetch a room's claude/* branches now, and put them where the hub's " +
	"checkout can merge them (`refs/remotes/<room>/claude/*`).\n\n" +
	"THE HUB DOES THIS ON ITS OWN every five minutes per attached room and when a room attaches. " +
	"Call it when a worker has just finished and you do not want to wait. Branches the room deleted " +
	"are pruned, and claude/main on a room is never collected. Nothing under refs/heads is written."

// registerGit adds the two tools to the control server being built. Through
// `addTool`, so they land on the full server only: neither is in `workerTools`.
func (c *controlMCP) registerGit(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_git_sync", Description: gitSyncToolDesc},
		audited(c, "ctl-git-sync", describeGitSync, c.gitSyncHandler))
	addTool(s, class, &mcp.Tool{Name: "atrium_git_collect", Description: gitCollectToolDesc},
		audited(c, "ctl-git-collect", describeGitCollect, c.gitCollectHandler))
	// In the worker set too (workerTools): the cards that push are workers.
	addTool(s, class, &mcp.Tool{Name: "atrium_git_push", Description: gitPushToolDesc},
		audited(c, "ctl-git-push", describeGitPush, c.gitPushHandler))
	// And the cards that read code they do not have. A read, so no audit line. See git_url.go.
	addTool(s, class, &mcp.Tool{Name: "atrium_git_url", Description: gitURLToolDesc}, c.gitURLHandler)
}

// describeGitSync names the room and repository, and whether it may have made a clone.
func describeGitSync(_ *mcp.CallToolRequest, in gitSyncInput, _ gitSyncOutput) (string, string, bool) {
	what := "git sync"
	if n := strings.TrimSpace(in.Name); n != "" {
		what += " " + n
	}
	if in.Init {
		what += " (init)"
	}
	return in.Room, what, true
}

func describeGitCollect(_ *mcp.CallToolRequest, in gitCollectInput, _ gitsync.CollectResult) (string, string, bool) {
	return in.Room, "git collect", true
}

const gitPushToolDesc = "Push your branch to the hub, the same push `git push hub <branch>` is, with no shell.\n\n" +
	"A PLAIN BRANCH PUSH ONLY: a branch name like `fix/x`. No force, no `+` refspec, no tag, no delete. The room " +
	"pushes from YOUR directory through its hub forwarder, with your card on the push, and the hub's own rules " +
	"apply (no non-fast-forward, nothing to `main`, a branch another card owns is refused). The answer is git's " +
	"report, and a refusal is the hub's sentence.\n\n" +
	"REFUSED when the room's git.push is none, when your clone's `hub` remote does not push to this room's " +
	"forwarder (after every url rewrite), when your card runs outside code, and when you are not a card on a room."

type gitPushInput struct {
	Branch string `json:"branch" jsonschema:"the branch to push, by name, like fix/x. nothing else is taken"`
}

type gitPushOutput struct {
	Card   string `json:"card"`
	Branch string `json:"branch"`
	Report string `json:"report" jsonschema:"git's own report of the push"`
}

func describeGitPush(_ *mcp.CallToolRequest, in gitPushInput, _ gitPushOutput) (string, string, bool) {
	return "", "git push " + in.Branch, true
}

// gitPushHandler is for the CALLER's own card: the push is run in its directory with its token, so a name is never
// taken for it. The caller is the `X-Atrium-Agent` claim, as for every tool here.
func (c *controlMCP) gitPushHandler(ctx context.Context, req *mcp.CallToolRequest, in gitPushInput) (
	*mcp.CallToolResult, gitPushOutput, error) {

	var out gitPushOutput
	room, me := roomOf(req), agentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("this pushes for a card, and nothing says which card is asking")
	}
	if why := gitsync.CheckPushBranch(strings.TrimSpace(in.Branch)); why != "" {
		return nil, out, fmt.Errorf("%s", why)
	}
	id, _, err := c.resolvePeer(ctx, room, me)
	if err != nil {
		return nil, out, err
	}
	err = c.longClient().ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/git-push", room,
		map[string]string{"branch": strings.TrimSpace(in.Branch)}, &out)
	if err != nil {
		var be *boardError
		if errors.As(err, &be) && be.bare && be.code == http.StatusNotFound {
			return nil, out, fmt.Errorf("this room predates atrium_git_push. update the room")
		}
	}
	return nil, out, err
}

func (c *controlMCP) longClient() *controlMCP {
	return &controlMCP{board: c.board, client: &http.Client{Timeout: gitTimeout, Transport: c.client.Transport}}
}

func (c *controlMCP) gitSyncHandler(ctx context.Context, _ *mcp.CallToolRequest, in gitSyncInput) (
	*mcp.CallToolResult, gitSyncOutput, error) {

	var out gitSyncOutput
	if strings.TrimSpace(in.Room) == "" {
		return nil, out, fmt.Errorf("say which room to sync")
	}
	err := c.longClient().ask(ctx, http.MethodPost, "/_hub/git/sync", "", map[string]any{
		"room": in.Room, "name": in.Name, "init": in.Init}, &out)
	return nil, out, err
}

func (c *controlMCP) gitCollectHandler(ctx context.Context, _ *mcp.CallToolRequest, in gitCollectInput) (
	*mcp.CallToolResult, gitsync.CollectResult, error) {

	var out gitsync.CollectResult
	if strings.TrimSpace(in.Room) == "" {
		return nil, out, fmt.Errorf("say which room to collect from")
	}
	err := c.longClient().ask(ctx, http.MethodPost, "/_hub/git/collect", "", map[string]any{"room": in.Room}, &out)
	return nil, out, err
}
