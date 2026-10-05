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
)

// atrium_backlog and atrium_reports, the control tools over /_hub/backlog and /_hub/reports. Full class only:
// `workerTools` does not name them. Both go through the hub, so a director on any room reads and writes the same rows.
// The HTTP half, and who may write, is in backlog.go. The audit lines are written there, so these tools write none.

const backlogToolDesc = "The backlog, kept on the hub, so a director on any room sees an item filed on any other. " +
	"Use it instead of a file under `docs/backlog` that only one room's checkout holds.\n\n" +
	"An item's id is the one you give, like `f-new-thing` or `r-037`, and is never reused. A department is " +
	"like `fabric`. Status is `open`, `held`, `in-progress`, `done` or `dropped`.\n\n" +
	"action is one of:\n" +
	"- `list {dept?, status?, open?}`: items without their bodies. `open` leaves out done and dropped.\n" +
	"- `get {id}`: one item with its body.\n" +
	"- `file {id, dept, title, body?, priority?}`: file one. An id that is taken is refused, and the item that " +
	"holds it is returned.\n" +
	"- `status {id, status}`: change the status."

type backlogInput struct {
	Action   string `json:"action" jsonschema:"list, get, file or status"`
	ID       string `json:"id,omitempty" jsonschema:"the item id, for get, file and status"`
	Dept     string `json:"dept,omitempty" jsonschema:"the department, for file, and to narrow a list"`
	Title    string `json:"title,omitempty" jsonschema:"for file: one line"`
	Body     string `json:"body,omitempty" jsonschema:"for file: the item, markdown"`
	Priority string `json:"priority,omitempty" jsonschema:"for file: HIGH, MEDIUM or the like"`
	Status   string `json:"status,omitempty" jsonschema:"for status: open, held, in-progress, done or dropped. for list: only that status"`
	Open     bool   `json:"open,omitempty" jsonschema:"for list: leave out done and dropped"`
}

type backlogOutput struct {
	Items []hubstore.BacklogItem `json:"items,omitempty"`
	Item  *hubstore.BacklogItem  `json:"item,omitempty"`
	Note  string                 `json:"note,omitempty"`
}

const reportsToolDesc = "Director reports, kept on the hub, so a report from a director on any room reaches the " +
	"orchestrator and the director it is for. Use it instead of `notes/director-reports.md`.\n\n" +
	"A report is append-only and is marked read once somebody has read it. `to` is the department it is for, " +
	"or leave it out for the orchestrator.\n\n" +
	"action is one of:\n" +
	"- `add {subject, body?, to?}`: leave a report. Answers its id.\n" +
	"- `list {to?, unread?, limit?}`: newest first, with bodies.\n" +
	"- `read {id}`: mark one read."

type reportsInput struct {
	Action  string `json:"action" jsonschema:"add, list or read"`
	ID      string `json:"id,omitempty" jsonschema:"the report id, for read"`
	To      string `json:"to,omitempty" jsonschema:"the department the report is for. for add, or to narrow a list"`
	Subject string `json:"subject,omitempty" jsonschema:"for add: one line"`
	Body    string `json:"body,omitempty" jsonschema:"for add: the report"`
	Unread  bool   `json:"unread,omitempty" jsonschema:"for list: only reports not yet read"`
	Limit   int    `json:"limit,omitempty" jsonschema:"for list: at most this many. default 100"`
}

type reportsOutput struct {
	Reports []hubstore.DirectorReport `json:"reports,omitempty"`
	Report  *hubstore.DirectorReport  `json:"report,omitempty"`
	Note    string                    `json:"note,omitempty"`
}

func (c *controlMCP) registerBacklog(s *mcp.Server, class ctlClass) {
	addTool(s, class, &mcp.Tool{Name: "atrium_backlog", Description: backlogToolDesc}, c.backlogHandler)
	addTool(s, class, &mcp.Tool{Name: "atrium_reports", Description: reportsToolDesc}, c.reportsHandler)
}

// backlogRoom is the room a call is from, for the row's filed_room. Empty when the caller is not one of atrium's sessions.
func backlogRoom(req *mcp.CallToolRequest) string {
	if r := roomOf(req); validRoomName(r) {
		return r
	}
	return ""
}

func (c *controlMCP) backlogHandler(ctx context.Context, req *mcp.CallToolRequest, in backlogInput) (
	*mcp.CallToolResult, backlogOutput, error) {

	var out backlogOutput
	id := strings.TrimSpace(in.ID)
	switch strings.TrimSpace(in.Action) {
	case "list":
		q := url.Values{}
		if d := strings.TrimSpace(in.Dept); d != "" {
			q.Set("dept", d)
		}
		if in.Status != "" {
			q.Set("status", in.Status)
		}
		if in.Open {
			q.Set("open", "1")
		}
		err := c.ask(ctx, http.MethodGet, "/_hub/backlog?"+q.Encode(), "", nil, &out)
		if err == nil && len(out.Items) == 0 {
			out.Note = "no backlog items match"
		}
		return nil, out, err
	case "get":
		if id == "" {
			return nil, out, &refusedError{"get needs id"}
		}
		var b hubstore.BacklogItem
		if err := c.ask(ctx, http.MethodGet, "/_hub/backlog/"+url.PathEscape(id), "", nil, &b); err != nil {
			return nil, out, err
		}
		out.Item = &b
		return nil, out, nil
	case "file":
		if id == "" || strings.TrimSpace(in.Dept) == "" || strings.TrimSpace(in.Title) == "" {
			return nil, out, &refusedError{"file needs id, dept and title"}
		}
		var b hubstore.BacklogItem
		err := c.ask(ctx, http.MethodPost, "/_hub/backlog", "", map[string]any{
			"id": id, "dept": strings.TrimSpace(in.Dept), "title": in.Title, "body": in.Body,
			"priority": in.Priority, "by": callerName(req), "room": backlogRoom(req)}, &b)
		if err != nil {
			return nil, out, err
		}
		out.Item = &b
		return nil, out, nil
	case "status":
		if id == "" || in.Status == "" {
			return nil, out, &refusedError{"status needs id and status"}
		}
		var b hubstore.BacklogItem
		err := c.ask(ctx, http.MethodPost, "/_hub/backlog/"+url.PathEscape(id), "", map[string]any{
			"status": in.Status, "by": callerName(req)}, &b)
		if err != nil {
			return nil, out, err
		}
		out.Item = &b
		return nil, out, nil
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of list, get, file, status", in.Action)}
}

func (c *controlMCP) reportsHandler(ctx context.Context, req *mcp.CallToolRequest, in reportsInput) (
	*mcp.CallToolResult, reportsOutput, error) {

	var out reportsOutput
	switch strings.TrimSpace(in.Action) {
	case "add":
		if strings.TrimSpace(in.Subject) == "" {
			return nil, out, &refusedError{"add needs subject"}
		}
		var r hubstore.DirectorReport
		err := c.ask(ctx, http.MethodPost, "/_hub/reports", "", map[string]any{
			"to": strings.TrimSpace(in.To), "subject": in.Subject, "body": in.Body,
			"by": callerName(req), "room": backlogRoom(req)}, &r)
		if err != nil {
			return nil, out, err
		}
		out.Report = &r
		return nil, out, nil
	case "list":
		q := url.Values{}
		if t := strings.TrimSpace(in.To); t != "" {
			q.Set("to", t)
		}
		if in.Unread {
			q.Set("unread", "1")
		}
		if in.Limit > 0 {
			q.Set("limit", strconv.Itoa(in.Limit))
		}
		err := c.ask(ctx, http.MethodGet, "/_hub/reports?"+q.Encode(), "", nil, &out)
		if err == nil && len(out.Reports) == 0 {
			out.Note = "no reports match"
		}
		return nil, out, err
	case "read":
		if strings.TrimSpace(in.ID) == "" {
			return nil, out, &refusedError{"read needs id"}
		}
		var r hubstore.DirectorReport
		err := c.ask(ctx, http.MethodPost, "/_hub/reports/"+url.PathEscape(strings.TrimSpace(in.ID)), "",
			map[string]any{"do": "read", "by": callerName(req)}, &r)
		if err != nil {
			return nil, out, err
		}
		out.Report = &r
		return nil, out, nil
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of add, list, read", in.Action)}
}
