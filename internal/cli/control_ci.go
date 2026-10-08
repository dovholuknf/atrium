package cli

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/link"
)

// atrium_ci on the stdio server. THE TOOL IS THE HUB'S (link.CIDoor): this file only says how this server reaches the
// hub's forge, which is this room's daemon, which asks the hub over its link (POST /v1/hub/ci,
// internal/daemon/hubci.go). Nothing here runs gh.

// ciWait bounds a CI question: the hub runs gh, and an artifact download may take a while.
const ciWait = 6 * time.Minute

var errOlderCIRoom = errors.New("this room predates atrium_ci on its own control server. update the room")

func addCITool(s *mcp.Server) {
	link.AddCITool(s, link.CIDoor{
		Ask: func(ctx context.Context, _ *mcp.CallToolRequest, in forge.HubCIAsk) (forge.HubCI, error) {
			var ans forge.HubCI
			err := askFor(ctx, ciWait, http.MethodPost, "/v1/hub/ci", in, &ans)
			if olderRoom(err) {
				return ans, errOlderCIRoom
			}
			return ans, err
		},
	})
}
