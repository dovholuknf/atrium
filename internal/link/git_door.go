package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// GitDoor is atrium_git_push and atrium_git_url, ONE CODE for the two servers that offer them: the hub's control over
// HTTP (controlMCP.gitDoor) and a room's stdio `atrium control` (internal/cli/control_git.go). What differs is only
// how each reaches the caller's room and the hub's lookup, which the door is given.
type GitDoor struct {
	// Card is the calling card's id, or the sentence saying there is none.
	Card func(ctx context.Context, req *mcp.CallToolRequest) (string, error)
	// IsCard says whether the caller is a card, whose URLs go on its room's forwarder.
	IsCard func(req *mcp.CallToolRequest) bool
	// Room asks the caller's room's board. long is a git push's bound rather than a quick call's.
	Room func(ctx context.Context, req *mcp.CallToolRequest, long bool, method, path string, body, out any) error
	// Lookup is the hub's answer to the query of GET /_hub/git/url.
	Lookup func(ctx context.Context, req *mcp.CallToolRequest, q url.Values) (gitsync.URLAnswer, error)
	// Older says whether an error is a bare 404, which is a room older than the endpoint.
	Older func(err error) bool
}

// AddGitTools puts both tools on a server, for a server that has no class or audit of its own (the stdio one). The
// hub's adds them through registerGit, with its audit line on the push.
func AddGitTools(s *mcp.Server, d GitDoor) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_git_push", Description: gitPushToolDesc}, d.Push)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_git_url", Description: gitURLToolDesc}, d.URL)
}

// Push is atrium_git_push: the room pushes one plain branch from the caller's own card's directory, with its token.
func (d GitDoor) Push(ctx context.Context, req *mcp.CallToolRequest, in gitPushInput) (
	*mcp.CallToolResult, gitPushOutput, error) {

	var out gitPushOutput
	branch := strings.TrimSpace(in.Branch)
	if why := gitsync.CheckPushBranch(branch); why != "" {
		return nil, out, fmt.Errorf("%s", why)
	}
	id, err := d.Card(ctx, req)
	if err != nil {
		return nil, out, err
	}
	err = d.Room(ctx, req, true, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/git-push",
		map[string]string{"branch": branch}, &out)
	if err != nil && d.Older != nil && d.Older(err) {
		return nil, out, fmt.Errorf("this room predates atrium_git_push. update the room")
	}
	return nil, out, err
}

// URL is atrium_git_url.
//
// A CARD FETCHES THROUGH ITS OWN ROOM'S FORWARDER. The URLs the lookup gives are on the address it reached the hub by,
// which is no address at all to a card on another room. So for a card the hub's URLs are rewritten onto its room's
// forwarder base (the path after /git/ is the same), the room being the one that knows its agent port. A room that
// does not say, or says something that is not a forwarder on its own loopback, leaves the card with no URL and the
// sentence why, not a wrong one. A room's work in progress has no forwarder route yet, so a card is given no URL for
// it (gitsync.ForCard).
func (d GitDoor) URL(ctx context.Context, req *mcp.CallToolRequest, in gitURLInput) (
	*mcp.CallToolResult, gitURLOutput, error) {

	var out gitURLOutput
	if strings.TrimSpace(in.Repo) == "" {
		return nil, out, fmt.Errorf("say which repository")
	}
	v := url.Values{"repo": {in.Repo}}
	if b := strings.TrimSpace(in.Branch); b != "" {
		v.Set("branch", b)
	}
	if r := strings.TrimSpace(in.Room); r != "" {
		v.Set("room", r)
	}
	ans, err := d.Lookup(ctx, req, v)
	if err != nil {
		return nil, out, err
	}
	out.URLAnswer = ans
	if d.IsCard(req) {
		var fw struct {
			Base string `json:"base"`
		}
		if err := d.Room(ctx, req, false, http.MethodGet, "/v1/hub-remote", nil, &fw); err != nil {
			fw.Base = ""
		}
		out.URLAnswer = out.URLAnswer.ForCard(forwarderBase(fw.Base))
	}
	out.Text = out.URLAnswer.Text()
	return nil, out, nil
}
