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

// NeedInput is atrium_need's parameters, the hub's own.
type NeedInput struct {
	Needs []string `json:"needs" jsonschema:"what you lack, each as mcp:<name>, agent:<name>, skill:<name>, tool:Skill, tool:Agent or tool:atrium_launch"`
	Why   string   `json:"why" jsonschema:"one line: why you need it, which the human is shown"`
}

// needHandler files the calling session's need with its own room. The hub's atrium_need also raises the growler.
func needHandler(ctx context.Context, _ *mcp.CallToolRequest, in NeedInput) (*mcp.CallToolResult, map[string]any, error) {
	out := map[string]any{}
	if strings.TrimSpace(in.Why) == "" {
		return nil, out, fmt.Errorf("say why you need it, in one line")
	}
	path, err := myCardPath("/need")
	if err != nil {
		return nil, out, err
	}
	err = askFor(ctx, 30*time.Second, http.MethodPost, path,
		map[string]any{"needs": in.Needs, "why": in.Why, "by": os.Getenv("ATRIUM_AGENT_NAME")}, &out)
	return nil, out, err
}

func addNeedTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_need", Description: link.NeedToolDesc}, needHandler)
}
