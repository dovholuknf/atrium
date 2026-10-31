package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

// atrium_item, the control tool an item's OWNER uses over /_hub/items. Full class only: `workerTools` does not name it,
// so no worker gets it and an item worker never learns an owner exists. The HTTP half, and who may act, is in items.go.

const itemToolDesc = "Items you own: work you started with `item` on a launch, or hold the role of. The hub keeps them.\n\n" +
	"atrium tells you ONCE when an item you own is blocked and once when it is finished, in a message from `atrium`. " +
	"A blocked item is not a finished one: answer it with `unblock`.\n\n" +
	"action is one of:\n" +
	"- `list {state?, all?}`: the items you own, newest change first. `all` lists every owner's. `state` is queued, " +
	"running, blocked or finished.\n" +
	"- `get {id}`: one item.\n" +
	"- `unblock {id, answer}`: type your answer to the worker, as `atrium`. The worker never learns who you are. The " +
	"item stays blocked until the worker's turn actually starts from your answer, and then it is running again.\n" +
	"- `cancel {id}`: end an item that is not finished, and its worker.\n" +
	"- `submit {pool, brief, owner?, room?}`: queue an item on a pool: resident cards tagged `atrium:pool:<name>`. A pool " +
	"is fed ONE ITEM AT A TIME PER CARD, in the order submitted, and an item waits until a card is idle. You own it unless " +
	"`owner` names `human` or a role. `room` is the pool's room, yours by default.\n" +
	"- `task_add {id, title, brief, outputs?, room?, model?, context?}`: a task of the item `id`: a throwaway low-context worker " +
	"in a folder atrium makes on a room, then removes once what lasts is kept. `brief` is the whole of what the worker is told " +
	"about the job. `outputs` are file names or globs, relative to the folder, that are kept on the hub. `context` sets the " +
	"item's task context, which every task's brief carries. Tasks of an item run one at a time, in the order added.\n" +
	"- `task_list {id}`: the item's tasks, with each one's state and result.\n" +
	"- `task_hold {id, task}`, `task_release {id, task}`: keep a pending task from starting, and let it start.\n" +
	"- `task_cancel {id, task}`: stop a task. A running one's worker is asked to leave and what it made is kept if it can be.\n" +
	"- `task_retry {id, task}`: a failed task again, as a new attempt in a new folder.\n" +
	"- `task_context {id, context}`: set the words every task's brief of that item carries.\n\n" +
	"Only the owner may unblock or cancel. An item the human owns has no unblock here: the human types into the worker's " +
	"terminal."

type itemInput struct {
	Action  string   `json:"action" jsonschema:"list, get, show, unblock, cancel or submit"`
	Pool    string   `json:"pool,omitempty" jsonschema:"for submit: the pool's name"`
	Brief   string   `json:"brief,omitempty" jsonschema:"for submit: the task, typed to whichever card takes it"`
	Owner   string   `json:"owner,omitempty" jsonschema:"for submit: human, orchestrator or director. empty is you"`
	Room    string   `json:"room,omitempty" jsonschema:"for submit: the room the pool is on. default is yours"`
	ID      string   `json:"id,omitempty" jsonschema:"the item id, for get, unblock and cancel"`
	Answer  string   `json:"answer,omitempty" jsonschema:"for unblock: what to type to the worker"`
	State   string   `json:"state,omitempty" jsonschema:"for list: only items in this state"`
	All     bool     `json:"all,omitempty" jsonschema:"for list: every owner's items and not only yours"`
	Task    string   `json:"task,omitempty" jsonschema:"for task_hold, task_release, task_cancel and task_retry: the task, like t1"`
	Title   string   `json:"title,omitempty" jsonschema:"for task_add: one line"`
	Outputs []string `json:"outputs,omitempty" jsonschema:"for task_add: files or globs, relative to the task folder, to keep on the hub"`
	Model   string   `json:"model,omitempty" jsonschema:"for task_add: the model the worker runs on. empty is the launch default"`
	Context string   `json:"context,omitempty" jsonschema:"for task_add and task_context: the words every task of this item carries in its brief, up to 2 KB"`
	Kind    string   `json:"kind,omitempty" jsonschema:"for task_add: scratch. code is not built yet"`
	Repo    string   `json:"repo,omitempty" jsonschema:"for task_add: reserved for code tasks"`
	Base    string   `json:"base,omitempty" jsonschema:"for task_add: reserved for code tasks"`
	After   []string `json:"after,omitempty" jsonschema:"for task_add: reserved for ordered tasks"`
}

type itemOutput struct {
	Items []hubstore.Item `json:"items,omitempty"`
	Item  *hubstore.Item  `json:"item,omitempty"`
	Tasks []hubstore.Task `json:"tasks,omitempty"`
	Note  string          `json:"note,omitempty"`
	// Lines and Folder are `show`: one line per step, and where the item's logs live.
	Lines  []string `json:"lines,omitempty"`
	Folder string   `json:"folder,omitempty"`
}

// ownerItemTools is atrium_item over one way to reach the hub's endpoints, as backlogTools is.
type ownerItemTools struct {
	call func(ctx context.Context, method, path string, body, out any) error
	by   func(req *mcp.CallToolRequest) string
}

func (c *controlMCP) registerItems(s *mcp.Server, class ctl.Class) {
	t := ownerItemTools{
		call: func(ctx context.Context, method, path string, body, out any) error {
			return c.ask(ctx, method, path, "", body, out)
		},
		by: callerName,
	}
	addTool(s, class, &mcp.Tool{Name: "atrium_item", Description: itemToolDesc}, t.itemHandler)
}

// AddRoomItemTools puts atrium_item on a room's stdio control server. call reaches the hub through the room, and by is
// the session's handle and room, which the hub stamps again from the link.
func AddRoomItemTools(s *mcp.Server, call func(ctx context.Context, method, path string, body, out any) error, by, room string) {
	t := ownerItemTools{call: call, by: func(*mcp.CallToolRequest) string {
		if by == "" || room == "" {
			return by
		}
		return by + "@" + room
	}}
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_item", Description: itemToolDesc}, t.itemHandler)
}

func (t ownerItemTools) itemHandler(ctx context.Context, req *mcp.CallToolRequest, in itemInput) (
	*mcp.CallToolResult, itemOutput, error) {

	var out itemOutput
	id := strings.TrimSpace(in.ID)
	switch strings.TrimSpace(in.Action) {
	case "list":
		q := url.Values{}
		if !in.All {
			q.Set("owner_kind", hubstore.OwnerSession)
			q.Set("owner", t.by(req))
		}
		if s := strings.TrimSpace(in.State); s != "" {
			q.Set("state", s)
		}
		err := t.call(ctx, http.MethodGet, "/_hub/items?"+q.Encode(), nil, &out)
		if err == nil && len(out.Items) == 0 {
			out.Note = "no items match"
		}
		return nil, out, err
	case "get":
		if id == "" {
			return nil, out, &ctl.RefusedError{Msg: "get needs id"}
		}
		var it hubstore.Item
		if err := t.call(ctx, http.MethodGet, "/_hub/items/"+url.PathEscape(id), nil, &it); err != nil {
			return nil, out, err
		}
		out.Item = &it
		return nil, out, nil
	case "show":
		if id == "" {
			return nil, out, &ctl.RefusedError{Msg: "show needs id: the backlog id, as at the top of your brief"}
		}
		d, err := FetchWorkDetail(ctx, t.call, id)
		if err != nil {
			return nil, out, err
		}
		out.Lines, out.Folder = StepLines(d), ItemFolder(d.Work.BacklogID)
		out.Note = d.Work.Title + " (" + d.Life.State + ")"
		return nil, out, nil
	case "unblock":
		if id == "" || strings.TrimSpace(in.Answer) == "" {
			return nil, out, &ctl.RefusedError{Msg: "unblock needs id and answer"}
		}
		return t.act(ctx, req, id, "unblock", in.Answer, "unblock typed to the worker. the item is running again once its turn starts from it")
	case "submit":
		if strings.TrimSpace(in.Pool) == "" || strings.TrimSpace(in.Brief) == "" {
			return nil, out, &ctl.RefusedError{Msg: "submit needs pool and brief"}
		}
		body := map[string]any{"do": "submit", "pool": in.Pool, "brief": in.Brief, "owner": in.Owner, "room": in.Room, "by": t.by(req)}
		var res struct {
			ItemID string `json:"item_id"`
			State  string `json:"state"`
		}
		if err := t.call(ctx, http.MethodPost, "/_hub/items", body, &res); err != nil {
			return nil, out, err
		}
		out.Note = "item " + res.ItemID + " is " + res.State + " on pool " + in.Pool + ". it is typed to a card when one is idle"
		return nil, out, nil
	case "cancel":
		if id == "" {
			return nil, out, &ctl.RefusedError{Msg: "cancel needs id"}
		}
		return t.act(ctx, req, id, "cancel", "", "cancel asked of the worker's room")
	}
	if strings.HasPrefix(strings.TrimSpace(in.Action), "task_") {
		return t.taskAction(ctx, req, in)
	}
	return nil, out, &ctl.RefusedError{Msg: fmt.Sprintf("action %q is not one of list, get, show, unblock, cancel, submit, task_add, task_list, "+
		"task_hold, task_release, task_cancel, task_retry, task_context", in.Action)}
}

func (t ownerItemTools) taskAction(ctx context.Context, req *mcp.CallToolRequest, in itemInput) (
	*mcp.CallToolResult, itemOutput, error) {

	var out itemOutput
	id := strings.TrimSpace(in.ID)
	action := strings.TrimPrefix(strings.TrimSpace(in.Action), "task_")
	if id == "" {
		return nil, out, &ctl.RefusedError{Msg: in.Action + " needs id: the backlog id of the item"}
	}
	path := "/_hub/items/" + url.PathEscape(id) + "/tasks"
	switch action {
	case "list":
		var res struct {
			Tasks     []hubstore.Task `json:"tasks"`
			Context   string          `json:"context"`
			Learnings []struct {
				Text string `json:"text"`
			} `json:"learnings"`
		}
		if err := t.call(ctx, http.MethodGet, path, nil, &res); err != nil {
			return nil, out, err
		}
		out.Tasks = res.Tasks
		out.Note = fmt.Sprintf("%d tasks, %d learnings", len(res.Tasks), len(res.Learnings))
		if res.Context != "" {
			out.Note += ". task context: " + res.Context
		}
		return nil, out, nil
	case "add":
		if strings.TrimSpace(in.Title) == "" || strings.TrimSpace(in.Brief) == "" {
			return nil, out, &ctl.RefusedError{Msg: "task_add needs title and brief"}
		}
		body := map[string]any{"do": "add", "by": t.by(req), "title": in.Title, "brief": in.Brief, "kind": in.Kind,
			"repo": in.Repo, "base": in.Base, "after": in.After, "outputs": in.Outputs, "room": in.Room, "model": in.Model,
			"context": in.Context}
		var res struct {
			Task hubstore.Task `json:"task"`
		}
		if err := t.call(ctx, http.MethodPost, path, body, &res); err != nil {
			return nil, out, err
		}
		out.Tasks = []hubstore.Task{res.Task}
		out.Note = "task " + res.Task.TaskID + " is " + res.Task.State + ". it starts when no other task of this item is running"
		return nil, out, nil
	case "context":
		if err := t.call(ctx, http.MethodPost, path, map[string]any{"do": "context", "by": t.by(req), "context": in.Context}, nil); err != nil {
			return nil, out, err
		}
		out.Note = "the item's task context is set"
		return nil, out, nil
	case "hold", "release", "cancel", "retry":
		if strings.TrimSpace(in.Task) == "" {
			return nil, out, &ctl.RefusedError{Msg: in.Action + " needs task, like t1"}
		}
		var res struct {
			Task hubstore.Task `json:"task"`
			Note string        `json:"note"`
		}
		if err := t.call(ctx, http.MethodPost, path, map[string]any{"do": action, "by": t.by(req), "task": in.Task}, &res); err != nil {
			return nil, out, err
		}
		out.Tasks = []hubstore.Task{res.Task}
		out.Note = "task " + res.Task.TaskID + " is " + res.Task.State
		if res.Note != "" {
			out.Note += ". " + res.Note
		}
		return nil, out, nil
	}
	return nil, out, &ctl.RefusedError{Msg: fmt.Sprintf("action %q is not one of task_add, task_list, task_hold, task_release, task_cancel, "+
		"task_retry, task_context", in.Action)}
}

func (t ownerItemTools) act(ctx context.Context, req *mcp.CallToolRequest, id, do, answer, note string) (
	*mcp.CallToolResult, itemOutput, error) {

	var out itemOutput
	body := map[string]any{"do": do, "by": t.by(req)}
	if answer != "" {
		body["answer"] = answer
	}
	if err := t.call(ctx, http.MethodPost, "/_hub/items/"+url.PathEscape(id), body, nil); err != nil {
		return nil, out, err
	}
	out.Note = note
	return nil, out, nil
}
