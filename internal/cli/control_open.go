package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_open: a link opened as a card, the way a paste on the board does. The room does the work
// (internal/api/open.go). This is the tool's face.

// openWait bounds the wait for an open, which may clone and fetch.
const openWait = 5 * time.Minute

type OpenInput struct {
	URL string `json:"url" jsonschema:"the link: a GitHub or Bitbucket pull request URL"`
	Why string `json:"why,omitempty" jsonschema:"why it is being opened, kept on the review and the card"`
}

type OpenOutput struct {
	Key      string `json:"key"`
	Card     string `json:"card"`
	PR       string `json:"pr,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Created  bool   `json:"created"`
	Room     string `json:"room,omitempty"`
}

const openToolDesc = "Open a pull request link as a card: its worktree, its review and a session started in the " +
	"worktree, on this room. The same as the operator pasting the link on the board.\n\n" +
	"A link that already has a live card answers that card with `created: false` and starts nothing. A link " +
	"another room holds is refused with that room's name.\n\n" +
	"Only pull requests so far. Any other link is refused with `not_a_pr`. A step that fails undoes what " +
	"the earlier steps made, and the answer says which step and why. Do not retry a refusal: tell whoever " +
	"you work for what it said."

func addOpenTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_open", Description: openToolDesc}, openHandler)
}

func openHandler(ctx context.Context, _ *mcp.CallToolRequest, in OpenInput) (*mcp.CallToolResult, OpenOutput, error) {
	out := OpenOutput{}
	if strings.TrimSpace(in.URL) == "" {
		return nil, out, fmt.Errorf("say which link to open")
	}
	if err := askFor(ctx, openWait, http.MethodPost, "/v1/open",
		map[string]string{"url": strings.TrimSpace(in.URL), "why": in.Why}, &out); err != nil {
		return nil, out, err
	}
	return nil, out, nil
}
