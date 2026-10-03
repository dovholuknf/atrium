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
)

// atrium_git_clone: a clone of a repository in the operator's scm folder, with atrium's `hub`
// remote on it. The room does the work (internal/daemon/gitclone.go). This is the tool's face.

// gitCloneWait bounds the wait for a clone.
const gitCloneWait = 13 * time.Minute

type GitCloneInput struct {
	URL string `json:"url" jsonschema:"the repository: https://host/owner/repo or git@host:owner/repo.git. Nothing else is taken"`
}

type GitCloneOutput struct {
	Path  string `json:"path,omitempty"`
	State string `json:"state"`
	Hub   string `json:"hub,omitempty"`
	Note  string `json:"note,omitempty"`
}

const gitCloneToolDesc = "Get a clone of a repository to work in: `<scm folder>/<host>/<owner>/<repo>` on this " +
	"room. If it is already there you get that one, and if not atrium clones it. Returns the path. " +
	"Your own worktree goes beside it.\n\n" +
	"Both `origin` (the forge, fetch only) and `hub` (atrium's, which `atrium_git_push` and `git push hub` use) " +
	"are set. If the clone's own `hub` remote points somewhere else it is left alone and atrium's is " +
	"`atrium-hub`: `hub` in the answer says which name to use.\n\n" +
	"A clone the OPERATOR made is not touched until the operator has said yes, once, on the board. The " +
	"answer is then `asked`: call again after they answer.\n\n" +
	"A repository atrium cannot clone (private with no credential, or a wrong name) fails with one " +
	"sentence. Do not retry it and do not ask for a paste: tell whoever you work for what it said.\n\n" +
	"There is no path, branch or option to pass. Only the URL."

func addGitCloneTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_git_clone", Description: gitCloneToolDesc}, gitCloneHandler)
}

func gitCloneHandler(ctx context.Context, _ *mcp.CallToolRequest, in GitCloneInput) (
	*mcp.CallToolResult, GitCloneOutput, error) {

	out := GitCloneOutput{}
	if strings.TrimSpace(in.URL) == "" {
		return nil, out, fmt.Errorf("say which repository to clone")
	}
	me := strings.TrimSpace(os.Getenv("ATRIUM_AGENT_NAME"))
	if me == "" {
		return nil, out, fmt.Errorf("atrium_git_clone is for a session atrium launched. this one has no " +
			"ATRIUM_AGENT_NAME, so there is no card to ask the operator for")
	}
	id, _, err := resolvePeer(ctx, me)
	if err != nil {
		return nil, out, err
	}
	// Clones take minutes on a slow link: the room bounds the git, and this bounds the wait.
	if err := askFor(ctx, gitCloneWait, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/git/clone",
		map[string]string{"url": in.URL}, &out); err != nil {
		return nil, out, err
	}
	return nil, out, nil
}
