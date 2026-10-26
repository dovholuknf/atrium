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

// atrium_backlog and atrium_reports, the control tools over /_hub/backlog and /_hub/reports. Full class has both,
// and `workerTools` names atrium_reports. Both go through the hub, so a director on any room reads and writes the same rows.
// The HTTP half, and who may write, is in backlog.go. The audit lines are written there, so these tools write none.

const backlogToolDesc = "The backlog, kept on the hub, so a director on any room sees an item filed on any other. " +
	"Use it instead of a file under `docs/backlog` that only one room's checkout holds.\n\n" +
	"An item's id is the one you give, like `f-new-thing` or `r-037`, and is never reused. A department is " +
	"like `fabric`. Status is `open`, `held`, `in-progress`, `done` or `dropped`.\n\n" +
	"action is one of:\n" +
	"- `list {dept?, status?, priority?, open?}`: items without their bodies. `open` leaves out done and dropped. " +
	"`priority` is `high`, `medium`, `low` or `unset`.\n" +
	"- `get {id}`: one item with its body.\n" +
	"- `file {id?, dept, title, body?, priority?}`: file one. An id that is taken is refused, and the item that " +
	"holds it is returned. Priority is `high`, `medium`, `low` or left out for unset: anything else is refused.\n" +
	"  A feature that left reference pages wrong files ONE item in department `docs` (ids `d-<n>`), whose body is " +
	"this block and nothing else: `feature:` the feature's item id, `changelog:` its fragment path, `shas:` the " +
	"branch tip, `new:` one line per verb, flag, endpoint, setting or tab, `stale:` `<path>  <what it says " +
	"wrong>` per page left wrong, `updated:` the pages the branch already fixed. No stale page, no item.\n" +
	"- `status {id, status}`: change the status.\n" +
	"- `priority {id, priority}`: change the priority to `high`, `medium`, `low` or `unset`.\n" +
	"- `edit {id, title?, body?}`: replace the title, the body, or both."

type backlogInput struct {
	Action   string `json:"action" jsonschema:"list, get, file, status, priority or edit"`
	ID       string `json:"id,omitempty" jsonschema:"the item id, for get and status, and for file when you choose it"`
	Dept     string `json:"dept,omitempty" jsonschema:"the department, for file, and to narrow a list"`
	Title    string `json:"title,omitempty" jsonschema:"for file and edit: one line"`
	Body     string `json:"body,omitempty" jsonschema:"for file and edit: the item, markdown"`
	Priority string `json:"priority,omitempty" jsonschema:"high, medium or low (unset for none). for file, priority, and to narrow a list"`
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

// backlogTools is the two tools over one way to reach the hub's endpoints. The hub's own control MCP asks its board
// over loopback. A room's stdio control asks its room, which carries the call over the link (hubask.go).
type backlogTools struct {
	call func(ctx context.Context, method, path string, body, out any) error
	// by and room are who is calling and from which room, for the row. See callerName and backlogRoom.
	by   func(req *mcp.CallToolRequest) string
	room func(req *mcp.CallToolRequest) string
}

// ask is the handlers' one way out. The room argument is not used: the hub's endpoints are not scoped to one.
func (b backlogTools) ask(ctx context.Context, method, path, _ string, body, out any) error {
	return b.call(ctx, method, path, body, out)
}

// tools is the two tools asking this hub's own board.
func (c *controlMCP) tools() backlogTools {
	return backlogTools{
		call: func(ctx context.Context, method, path string, body, out any) error {
			return c.ask(ctx, method, path, "", body, out)
		},
		by: callerName, room: backlogRoom,
	}
}

func (c *controlMCP) registerBacklog(s *mcp.Server, class ctlClass) {
	t := c.tools()
	addTool(s, class, &mcp.Tool{Name: "atrium_backlog", Description: backlogToolDesc}, t.backlogHandler)
	addTool(s, class, &mcp.Tool{Name: "atrium_reports", Description: reportsToolDesc}, t.reportsHandler)
}

// AddRoomBacklogTools puts atrium_backlog and atrium_reports on a room's stdio control server. call reaches the hub's
// /_hub/backlog and /_hub/reports through the room, and by and room are the session's own handle and room name, which
// the stdio server has no request headers to read. The hub stamps both again from the link, so they are only a label.
func AddRoomBacklogTools(s *mcp.Server, call func(ctx context.Context, method, path string, body, out any) error,
	by, room string) {

	t := backlogTools{
		call: call,
		by: func(*mcp.CallToolRequest) string {
			if by == "" || room == "" {
				return by
			}
			return by + "@" + room
		},
		room: func(*mcp.CallToolRequest) string {
			if validRoomName(room) {
				return room
			}
			return ""
		},
	}
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_backlog", Description: backlogToolDesc}, t.backlogHandler)
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_reports", Description: reportsToolDesc}, t.reportsHandler)
}

// backlogRoom is the room a call is from, for the row's filed_room. Empty when the caller is not one of atrium's sessions.
func backlogRoom(req *mcp.CallToolRequest) string {
	if r := roomOf(req); validRoomName(r) {
		return r
	}
	return ""
}

func (c backlogTools) backlogHandler(ctx context.Context, req *mcp.CallToolRequest, in backlogInput) (
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
		if p := strings.TrimSpace(in.Priority); p != "" {
			q.Set("priority", p)
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
		if strings.TrimSpace(in.Dept) == "" || strings.TrimSpace(in.Title) == "" {
			return nil, out, &refusedError{"file needs dept and title"}
		}
		var b hubstore.BacklogItem
		err := c.ask(ctx, http.MethodPost, "/_hub/backlog", "", map[string]any{
			"id": id, "dept": strings.TrimSpace(in.Dept), "title": in.Title, "body": in.Body,
			"priority": in.Priority, "by": c.by(req), "room": c.room(req)}, &b)
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
			"status": in.Status, "by": c.by(req)}, &b)
		if err != nil {
			return nil, out, err
		}
		out.Item = &b
		return nil, out, nil
	case "priority":
		if id == "" || strings.TrimSpace(in.Priority) == "" {
			return nil, out, &refusedError{"priority needs id and priority (" + hubstore.PriorityWords + ", say unset for none)"}
		}
		var b hubstore.BacklogItem
		err := c.ask(ctx, http.MethodPost, "/_hub/backlog/"+url.PathEscape(id), "", map[string]any{
			"priority": in.Priority, "by": c.by(req)}, &b)
		if err != nil {
			return nil, out, err
		}
		out.Item = &b
		return nil, out, nil
	case "edit":
		if id == "" || (in.Title == "" && in.Body == "") {
			return nil, out, &refusedError{"edit needs id and a title or a body"}
		}
		ask := map[string]any{"by": c.by(req)}
		if in.Title != "" {
			ask["title"] = in.Title
		}
		if in.Body != "" {
			ask["body"] = in.Body
		}
		var b hubstore.BacklogItem
		if err := c.ask(ctx, http.MethodPost, "/_hub/backlog/"+url.PathEscape(id), "", ask, &b); err != nil {
			return nil, out, err
		}
		out.Item = &b
		return nil, out, nil
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of list, get, file, status, priority, edit", in.Action)}
}

func (c backlogTools) reportsHandler(ctx context.Context, req *mcp.CallToolRequest, in reportsInput) (
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
			"by": c.by(req), "room": c.room(req)}, &r)
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
			map[string]any{"do": "read", "by": c.by(req)}, &r)
		if err != nil {
			return nil, out, err
		}
		out.Report = &r
		return nil, out, nil
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of add, list, read", in.Action)}
}

// ItemTagPrefix is what a launch tag starts with to name the backlog item the card works on: `item:f-new-thing`.
const ItemTagPrefix = "item:"

// itemOfTags is the item id a launch's tags name, or "".
func itemOfTags(tags []string) string {
	for _, t := range tags {
		if id, ok := strings.CutPrefix(strings.TrimSpace(t), ItemTagPrefix); ok && backlogIDRe.MatchString(id) {
			return id
		}
	}
	return ""
}

// linkItemToCard puts the item a launch names in progress with the card it started. Best effort: a hub with no
// backlog, or an id the hub does not hold, is no reason to fail a launch that already happened, so the answer is a
// sentence for the launch's note and "" when there is nothing to say.
func (c *controlMCP) linkItemToCard(ctx context.Context, tags []string, room, card, by string) string {
	id := itemOfTags(tags)
	if id == "" {
		return ""
	}
	var b hubstore.BacklogItem
	err := c.ask(ctx, http.MethodPost, "/_hub/backlog/"+url.PathEscape(id), "", map[string]any{
		"card": tagFor(room, card), "by": by}, &b)
	if err != nil {
		return "the item " + id + " was not moved to in-progress: " + err.Error() + ". "
	}
	return ""
}

// followItem is a card's report reaching the item linked to it. The hub answers followed false for a card with no
// item, which is most of them, and a failure here never fails the report.
func (c *controlMCP) followItem(ctx context.Context, room, card, status, by string) {
	if _, ok := hubstore.FollowStatus(status); !ok {
		return
	}
	var out struct {
		Followed bool `json:"followed"`
	}
	_ = c.ask(ctx, http.MethodPost, "/_hub/backlog/follow", "", map[string]any{
		"card": tagFor(room, card), "status": strings.TrimSpace(status), "by": by}, &out)
}
