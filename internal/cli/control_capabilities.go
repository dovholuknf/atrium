package cli

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link"
)

// The tool is the hub's. This only reaches it through this room's daemon, which asks the hub over its link.

func roomCapabilityAsk(ctx context.Context, method, path string, out any) error {
	err := askFor(ctx, hubAskWait, http.MethodPost, "/v1/hub/ask", map[string]any{
		"from": strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME")), "method": method, "path": path}, out)
	if olderRoom(err) {
		return &peerToolErr{code: http.StatusNotFound, msg: "this room predates the hub backlog on its own control server. update the room"}
	}
	return err
}

func addCapabilityTool(s *mcp.Server) {
	link.AddRoomCapabilityTool(s, roomCapabilityAsk)
}

// placeByNeeds is the room a stdio launch with needs goes to, read from the hub's matrix.
func placeByNeeds(ctx context.Context, in LaunchInput) (string, error) {
	var v link.CapabilityView
	if err := roomCapabilityAsk(ctx, http.MethodGet, "/_hub/capabilities", &v); err != nil {
		return "", fmt.Errorf("could not read the rooms' capabilities to place this launch: %w", err)
	}
	own, named := strings.TrimSpace(os.Getenv("ATRIUM_ROOM")), strings.TrimSpace(in.Room) != ""
	want := own
	if named {
		want = strings.TrimSpace(in.Room)
	}
	return link.RoomForNeeds(v, in.Needs, want, named, time.Now())
}
