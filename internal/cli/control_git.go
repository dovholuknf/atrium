package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/link"
)

// atrium_git_push and atrium_git_url on the stdio server, for a card on a room whose sessions run `atrium control`
// rather than the hub's HTTP one. THE TOOLS ARE THE HUB'S (link.GitDoor): this file only says how this server
// reaches the room, which is this machine's daemon, and the hub's lookup, which the room asks over its link
// (GET /v1/hub/git/url, internal/daemon/hubremote.go).

// gitURLWait bounds a lookup: the room asks the hub, and the hub asks every room that may have the branch.
const gitURLWait = gitsync.LookupRoomWait + 20*time.Second

func addGitTools(s *mcp.Server) {
	link.AddGitTools(s, stdioGitDoor())
}

func stdioGitDoor() link.GitDoor {
	return link.GitDoor{
		Card: func(ctx context.Context, _ *mcp.CallToolRequest) (string, error) {
			me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))
			if me == "" {
				return "", fmt.Errorf("atrium_git_push is for a session atrium launched. this one has no " +
					"ATRIUM_AGENT_NAME, so there is no card to push for")
			}
			id, _, err := resolvePeer(ctx, me)
			return id, err
		},
		// Every caller here is on this room, so its URLs go on this room's forwarder, which is the only address
		// this machine has for the hub. One with no card token is refused by the forwarder, with its sentence.
		IsCard: func(*mcp.CallToolRequest) bool { return true },
		Room: func(ctx context.Context, _ *mcp.CallToolRequest, long bool, method, path string, body, out any) error {
			wait := peerToolTimeout
			if long {
				wait = link.GitWait
			}
			return askFor(ctx, wait, method, path, body, out)
		},
		Lookup: func(ctx context.Context, _ *mcp.CallToolRequest, q url.Values) (gitsync.URLAnswer, error) {
			var ans gitsync.URLAnswer
			err := askFor(ctx, gitURLWait, http.MethodGet, "/v1/hub/git/url?"+q.Encode(), nil, &ans)
			if olderRoom(err) {
				return ans, fmt.Errorf("this room predates atrium_git_url on its own control server. update the room")
			}
			return ans, err
		},
		Older: olderRoom,
	}
}
