package link

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// atrium_worktrees, the control tool over the room's /v1/worktrees/*. Full class only: `workerTools` does not name it,
// so a worker never sees it. The room runs every git command and the hub runs none. The answer is the room's text and
// nothing else, since a second copy of the same facts as JSON doubles what every later turn reads. See
// docs/runtime/worktrees-tool-design.md.

const worktreesToolDesc = "Audit a repository's git worktrees in one call, changing nothing.\n\n" +
	"One line per worktree: path, branch, dirty count, whether its commits landed on claude/main " +
	"(ancestry, patch id, or subject with the same files), and a verdict, `removable` or `keep: <reasons>`. " +
	"Use it instead of scripting `git worktree list` and `git status`.\n\n" +
	"`repo` is a path inside any checkout or host/org/repo, default your cwd. `prefix` or `paths` narrow " +
	"it, never both. `room` defaults to yours. action `remove` is declared and answers not built yet."

// worktreesTimeout is the room's whole bound, 60 seconds, and a little over for the trip.
const worktreesTimeout = 70 * time.Second

type worktreesInput struct {
	Action string   `json:"action" jsonschema:"audit or remove"`
	Room   string   `json:"room,omitempty" jsonschema:"the room whose worktrees. default your own"`
	Repo   string   `json:"repo,omitempty" jsonschema:"a path inside any checkout of the repo, or host/org/repo. default your cwd"`
	Prefix string   `json:"prefix,omitempty" jsonschema:"audit: only worktrees whose path starts with this. not with paths"`
	Paths  []string `json:"paths,omitempty" jsonschema:"audit: only these worktrees. remove: required, these and nothing else"`
}

func (c *controlMCP) registerWorktrees(s *mcp.Server, class ctl.Class) {
	addTool(s, class, &mcp.Tool{Name: "atrium_worktrees", Description: worktreesToolDesc}, c.worktreesHandler)
}

func (c *controlMCP) worktreesHandler(ctx context.Context, req *mcp.CallToolRequest, in worktreesInput) (
	*mcp.CallToolResult, any, error) {

	switch strings.TrimSpace(in.Action) {
	case "audit":
	case "remove":
		return nil, nil, &ctl.RefusedError{Msg: "remove is not built yet. audit tells what is removable, and atrium_cull " +
			"reclaims a finished worker"}
	default:
		return nil, nil, &ctl.RefusedError{Msg: "action is audit or remove"}
	}
	if strings.TrimSpace(in.Prefix) != "" && len(in.Paths) > 0 {
		return nil, nil, &ctl.RefusedError{Msg: "prefix and paths together are refused: give one"}
	}
	room := strings.TrimSpace(in.Room)
	if room == "" {
		room = ctl.RoomOf(req)
	}
	long := &controlMCP{board: c.board, client: &http.Client{Timeout: worktreesTimeout, Transport: c.client.Transport}}
	var res struct {
		Text string `json:"text"`
	}
	err := long.Ask(ctx, http.MethodPost, "/v1/worktrees/audit", room, map[string]any{
		"repo": in.Repo, "prefix": in.Prefix, "paths": in.Paths, "caller": ctl.AgentOf(req)}, &res)
	if err != nil {
		var be *ctl.BoardError
		if errors.As(err, &be) && be.NoSentence && be.Code == http.StatusNotFound {
			return nil, nil, errors.New("that room is older than atrium_worktrees. update the room")
		}
		return nil, nil, err
	}
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: strings.TrimRight(res.Text, "\n")}}}, nil, nil
}
