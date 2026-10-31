package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// atrium_merge, ONE CODE for the hub's control over HTTP and a room's stdio `atrium control`, over /_hub/merge
// (merge.go). In the worker set, since workers are the ones that ask. Setting the merge room and landing are the
// operator's and are not actions here.

const mergeToolDesc = "The merge queue: land your claude/* branch on claude/main. Push the branch to the hub first, " +
	"then ask. The hub merges it onto claude/main, the merge room runs scripts/test-touched.sh on the merge, and on " +
	"green claude/main moves and your branch is deleted. One request at a time, in order. You and your launcher hear " +
	"`merged <sha>`, or red with the failing tests, or a conflict, or refused with the reason. Never push claude/main " +
	"yourself.\n\n" +
	"action is one of:\n" +
	"- `request {branch?, repo?, sha?}`: queue one. On a room's control the branch, the repository and the sha are " +
	"your worktree's current ones when left out. Asking again answers the open request.\n" +
	"- `list {repo?, all?}`: the open requests first, then the latest. `all` is up to 500.\n" +
	"- `show {n}`: one request with its test output.\n" +
	"- `cancel {n}`: take back a request of yours that is still queued."

type mergeInput struct {
	Action string `json:"action" jsonschema:"request, list, show or cancel"`
	Branch string `json:"branch,omitempty" jsonschema:"for request: the claude/* branch. default: your worktree's branch"`
	Repo   string `json:"repo,omitempty" jsonschema:"for request: <host>/<owner>/<repo>. default: your worktree's. narrows a list"`
	SHA    string `json:"sha,omitempty" jsonschema:"for request: the tip you pushed. default: your worktree's HEAD"`
	N      int64  `json:"n,omitempty" jsonschema:"the request number, for show and cancel"`
	All    bool   `json:"all,omitempty" jsonschema:"for list: up to 500, not 50"`
	Of     int64  `json:"of,omitempty" jsonschema:"for request by a merger: the request in conflict it merged"`
}

type mergeOutput struct {
	Request  *hubstore.MergeRequest  `json:"request,omitempty"`
	Requests []hubstore.MergeRequest `json:"requests,omitempty"`
	Existing bool                    `json:"existing,omitempty"`
	Held     map[string]string       `json:"held,omitempty"`
	Room     string                  `json:"room,omitempty"`
	Why      string                  `json:"why,omitempty"`
	Note     string                  `json:"note,omitempty"`
}

// MergeHere is the repository, branch and sha of the caller's worktree, for a request that leaves them out.
type MergeHere func(ctx context.Context) (repo, branch, sha string, err error)

// MergeDoor is atrium_merge for one server. Call reaches /_hub/merge, By is the caller as <card>@<room>, Here the
// caller's worktree, nil where the server has none.
type MergeDoor struct {
	Call func(ctx context.Context, method, path string, body, out any) error
	By   func(req *mcp.CallToolRequest) string
	Here MergeHere
}

// AddRoomMergeTool puts atrium_merge on a room's stdio control server.
func AddRoomMergeTool(s *mcp.Server, d MergeDoor) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_merge", Description: mergeToolDesc}, d.Handle)
}

func (c *controlMCP) registerMerge(s *mcp.Server, class ctl.Class) {
	d := MergeDoor{
		Call: func(ctx context.Context, method, path string, body, out any) error {
			return c.ask(ctx, method, path, "", body, out)
		},
		By: callerName,
	}
	addTool(s, class, &mcp.Tool{Name: "atrium_merge", Description: mergeToolDesc}, d.Handle)
}

// Handle answers one call.
func (d MergeDoor) Handle(ctx context.Context, req *mcp.CallToolRequest, in mergeInput) (*mcp.CallToolResult, mergeOutput, error) {
	var out mergeOutput
	switch strings.TrimSpace(in.Action) {
	case "request":
		repo, branch, sha := strings.TrimSpace(in.Repo), strings.TrimSpace(in.Branch), strings.TrimSpace(in.SHA)
		if (repo == "" || branch == "") && d.Here != nil {
			hr, hb, hs, err := d.Here(ctx)
			if err != nil {
				return nil, out, &ctl.RefusedError{Msg: err.Error() + ". name the branch and the repository"}
			}
			if repo == "" {
				repo = hr
			}
			if branch == "" {
				branch, sha = hb, orDefaultStr(sha, hs)
			}
		}
		if repo == "" || branch == "" {
			return nil, out, &ctl.RefusedError{Msg: "request needs branch and repo"}
		}
		by := ""
		if d.By != nil {
			by = d.By(req)
		}
		err := d.Call(ctx, http.MethodPost, "/_hub/merge", map[string]any{"repo": repo, "branch": branch, "sha": sha, "by": by, "of": in.Of}, &out)
		if err == nil && out.Request != nil {
			out.Note = fmt.Sprintf("request %d is %s. you will be told how it ends", out.Request.N, out.Request.State)
		}
		return nil, out, err
	case "list":
		q := url.Values{}
		if r := strings.TrimSpace(in.Repo); r != "" {
			q.Set("repo", r)
		}
		if in.All {
			q.Set("all", "1")
		}
		err := d.Call(ctx, http.MethodGet, "/_hub/merge?"+q.Encode(), nil, &out)
		if err == nil && len(out.Requests) == 0 {
			out.Note = "the merge queue has no requests"
		}
		return nil, out, err
	case "show":
		if in.N <= 0 {
			return nil, out, &ctl.RefusedError{Msg: "show needs n"}
		}
		var m hubstore.MergeRequest
		if err := d.Call(ctx, http.MethodGet, "/_hub/merge/"+strconv.FormatInt(in.N, 10), nil, &m); err != nil {
			return nil, out, err
		}
		out.Request = &m
		return nil, out, nil
	case "cancel":
		if in.N <= 0 {
			return nil, out, &ctl.RefusedError{Msg: "cancel needs n"}
		}
		by := ""
		if d.By != nil {
			by = d.By(req)
		}
		if by == "" {
			return nil, out, &ctl.RefusedError{Msg: "a card cancels its own request. the operator cancels with atrium merge cancel"}
		}
		var m hubstore.MergeRequest
		if err := d.Call(ctx, http.MethodPost, "/_hub/merge/"+strconv.FormatInt(in.N, 10)+"/cancel", map[string]any{"by": by}, &m); err != nil {
			return nil, out, err
		}
		out.Request = &m
		return nil, out, nil
	}
	return nil, out, &ctl.RefusedError{Msg: "action is request, list, show or cancel"}
}

func orDefaultStr(v, def string) string {
	if strings.TrimSpace(v) != "" {
		return v
	}
	return def
}
