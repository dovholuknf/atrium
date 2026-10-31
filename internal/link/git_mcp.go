package link

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// ── control tools ───────────────────────────────────────

// gitTimeout is how long a control tool waits for a sync or a collect. A first clone over
// an overlay is minutes.
const gitTimeout = gitsync.CommandBound + 2*time.Minute

// GitWait is gitTimeout for the stdio server's push, which waits on the same git.
const GitWait = gitTimeout

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
func (c *controlMCP) registerGit(s *mcp.Server, class ctl.Class) {
	addTool(s, class, &mcp.Tool{Name: "atrium_git_sync", Description: gitSyncToolDesc},
		audited(c, "ctl-git-sync", describeGitSync, c.gitSyncHandler))
	addTool(s, class, &mcp.Tool{Name: "atrium_git_collect", Description: gitCollectToolDesc},
		audited(c, "ctl-git-collect", describeGitCollect, c.gitCollectHandler))
	// In the worker set too (workerTools): the cards that push are workers.
	addTool(s, class, &mcp.Tool{Name: "atrium_git_push", Description: gitPushToolDesc},
		audited(c, "ctl-git-push", describeGitPush, c.gitPushHandler))
	// And the cards that read code they do not have. A read, so no audit line. See git_url.go.
	addTool(s, class, &mcp.Tool{Name: "atrium_git_url", Description: gitURLToolDesc}, c.gitURLHandler)
	// And the cards that read a repository's CI, which the hub reads for them. A read, so no audit line. See ci_door.go.
	addTool(s, class, &mcp.Tool{Name: "atrium_ci", Description: ciToolDesc}, c.ciHandler)
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
// taken for it. The caller is the `X-Atrium-Agent` claim, as for every tool here. The tool itself is GitDoor.Push.
func (c *controlMCP) gitPushHandler(ctx context.Context, req *mcp.CallToolRequest, in gitPushInput) (
	*mcp.CallToolResult, gitPushOutput, error) {
	return c.gitDoor().Push(ctx, req, in)
}

// gitDoor is atrium_git_push and atrium_git_url as the hub serves them: the caller's card and room from its headers,
// the room asked through the hub's board, and the lookup the hub's own.
func (c *controlMCP) gitDoor() GitDoor {
	return GitDoor{
		Card: func(ctx context.Context, req *mcp.CallToolRequest) (string, error) {
			me := ctl.AgentOf(req)
			if me == "" {
				return "", fmt.Errorf("this pushes for a card, and nothing says which card is asking")
			}
			id, _, err := c.ResolvePeer(ctx, ctl.RoomOf(req), me)
			return id, err
		},
		IsCard: func(req *mcp.CallToolRequest) bool { return ctl.AgentOf(req) != "" },
		Room: func(ctx context.Context, req *mcp.CallToolRequest, long bool, method, path string, body, out any) error {
			cl := c
			if long {
				cl = c.longClient()
			}
			return cl.Ask(ctx, method, path, ctl.RoomOf(req), body, out)
		},
		Lookup: func(ctx context.Context, _ *mcp.CallToolRequest, q url.Values) (gitsync.URLAnswer, error) {
			var ans gitsync.URLAnswer
			err := c.Ask(ctx, http.MethodGet, "/_hub/git/url?"+q.Encode(), "", nil, &ans)
			return ans, err
		},
		Older: func(err error) bool {
			var be *ctl.BoardError
			return errors.As(err, &be) && be.NoSentence && be.Code == http.StatusNotFound
		},
	}
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
	err := c.longClient().Ask(ctx, http.MethodPost, "/_hub/git/sync", "", map[string]any{
		"room": in.Room, "name": in.Name, "init": in.Init}, &out)
	return nil, out, err
}

func (c *controlMCP) gitCollectHandler(ctx context.Context, _ *mcp.CallToolRequest, in gitCollectInput) (
	*mcp.CallToolResult, gitsync.CollectResult, error) {

	var out gitsync.CollectResult
	if strings.TrimSpace(in.Room) == "" {
		return nil, out, fmt.Errorf("say which room to collect from")
	}
	err := c.longClient().Ask(ctx, http.MethodPost, "/_hub/git/collect", "", map[string]any{"room": in.Room}, &out)
	return nil, out, err
}
