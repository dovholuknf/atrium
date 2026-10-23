package cli

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link"
)

// atrium_backlog and atrium_reports on the stdio server. THE TOOLS ARE THE HUB'S (link.AddRoomBacklogTools): this file
// only says how this server reaches the hub's rows, which is this room's daemon, which asks the hub over its link
// (POST /v1/hub/ask, internal/daemon/hubask.go). The hub stamps who asked from the link.

// hubAskWait bounds one call: this room, the link, the hub's board and back.
const hubAskWait = 60 * time.Second

func addBacklogTools(s *mcp.Server) {
	me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))
	room := strings.TrimSpace(os.Getenv("ATRIUM_ROOM"))
	call := func(ctx context.Context, method, path string, body, out any) error {
		err := askFor(ctx, hubAskWait, http.MethodPost, "/v1/hub/ask",
			map[string]any{"from": me, "method": method, "path": path, "body": body}, out)
		if olderRoom(err) {
			return &peerToolErr{code: http.StatusNotFound, msg: "this room predates the hub backlog on its own " +
				"control server. update the room"}
		}
		return err
	}
	link.AddRoomBacklogTools(s, call, me, room)
	// atrium_item rides the same way to the hub. See link/items_mcp.go.
	link.AddRoomItemTools(s, call, me, room)
	// And atrium_publish, which every worker prompt names. See link/docs_mcp.go.
	link.AddRoomPublishTool(s, call, me)
	// And atrium_merge, from the session's own worktree. See link/merge_mcp.go.
	link.AddRoomMergeTool(s, link.MergeDoor{
		Call: call,
		By:   func(*mcp.CallToolRequest) string { return me },
		Here: func(ctx context.Context) (string, string, string, error) {
			dir, _ := os.Getwd()
			return mergeHere(ctx, dir)
		},
	})
}
