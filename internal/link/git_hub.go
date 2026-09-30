package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The hub's git side, as the board API and the control tools see it. See git.go for the
// transport and internal/gitsync for what actually runs.
//
// NO ROUTE AND NO TOOL HERE TAKES A PATH, A URL, A REFSPEC OR A BRANCH. A room, a repository
// name from `git_repos`, and whether to init. Everything else is the operator's setting.

// GitRooms adapts this hub to what internal/gitsync needs of the link.
func (h *Hub) GitRooms() gitsync.Rooms { return hubGitRooms{h} }

type hubGitRooms struct{ h *Hub }

func (g hubGitRooms) Attached() []gitsync.RoomInfo {
	var out []gitsync.RoomInfo
	for _, a := range g.h.Rooms() {
		out = append(out, gitsync.RoomInfo{Name: a.Name, Git: a.Git})
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

// serveGit answers /_hub/git/{sync,collect,status}. Loopback only, like the control server:
// these start work on a room, and an overlay is not an auth layer.
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
	if !loopbackRemote(r.RemoteAddr) {
		fail(http.StatusForbidden, "git sync is started only from the machine the hub runs on")
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

// registerGit adds the two tools to the control server.
func (c *controlMCP) registerGit(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_git_sync", Description: gitSyncToolDesc}, c.gitSyncHandler)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_git_collect", Description: gitCollectToolDesc}, c.gitCollectHandler)
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
