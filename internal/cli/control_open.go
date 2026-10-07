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
	URL  string `json:"url" jsonschema:"the link: a pull request, issue or branch URL, or a support ticket or forum topic"`
	Why  string `json:"why,omitempty" jsonschema:"why it is being opened, kept on the review and the card"`
	Repo string `json:"repo,omitempty" jsonschema:"for a link that names no repo: host/org/repo, or none for a scratch folder. empty takes the recogniser's default"`
}

type OpenOutput struct {
	Key      string `json:"key"`
	Kind     string `json:"kind,omitempty"`
	Card     string `json:"card"`
	PR       string `json:"pr,omitempty"`
	Worktree string `json:"worktree,omitempty"`
	Repo     string `json:"repo,omitempty"`
	Created  bool   `json:"created"`
	Room     string `json:"room,omitempty"`
}

const openToolDesc = "Open a link as a card, on this room. The same as the operator pasting the link on the board. " +
	"A pull request gets its worktree, its review and a session in the worktree. An issue or a branch gets a " +
	"worktree on its branch and a session. A support ticket or forum topic names no repo: it opens in `repo`, " +
	"else the recogniser's default repo, and `repo: none` is a scratch folder of the card's own.\n\n" +
	"A link that already has a live card answers that card with `created: false` and starts nothing. A pull " +
	"request another room holds is refused with that room's name.\n\n" +
	"A link that names no piece of work (a repo page) is refused with `not_openable`. A step that fails undoes " +
	"what the earlier steps made, and the answer says which step and why. Do not retry a refusal: tell whoever " +
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
		map[string]string{"url": strings.TrimSpace(in.URL), "why": in.Why, "repo": strings.TrimSpace(in.Repo)}, &out); err != nil {
		return nil, out, err
	}
	return nil, out, nil
}
