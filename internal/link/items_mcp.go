package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/hubstore"
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
	"`owner` names `human` or a role. `room` is the pool's room, yours by default.\n\n" +
	"Only the owner may unblock or cancel. An item the human owns has no unblock here: the human types into the worker's " +
	"terminal."

type itemInput struct {
	Action string `json:"action" jsonschema:"list, get, show, unblock, cancel or submit"`
	Pool   string `json:"pool,omitempty" jsonschema:"for submit: the pool's name"`
	Brief  string `json:"brief,omitempty" jsonschema:"for submit: the task, typed to whichever card takes it"`
	Owner  string `json:"owner,omitempty" jsonschema:"for submit: human, orchestrator, merger or director. empty is you"`
	Room   string `json:"room,omitempty" jsonschema:"for submit: the room the pool is on. default is yours"`
	ID     string `json:"id,omitempty" jsonschema:"the item id, for get, unblock and cancel"`
	Answer string `json:"answer,omitempty" jsonschema:"for unblock: what to type to the worker"`
	State  string `json:"state,omitempty" jsonschema:"for list: only items in this state"`
	All    bool   `json:"all,omitempty" jsonschema:"for list: every owner's items and not only yours"`
}

type itemOutput struct {
	Items []hubstore.Item `json:"items,omitempty"`
	Item  *hubstore.Item  `json:"item,omitempty"`
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

func (c *controlMCP) registerItems(s *mcp.Server, class ctlClass) {
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
			return nil, out, &refusedError{"get needs id"}
		}
		var it hubstore.Item
		if err := t.call(ctx, http.MethodGet, "/_hub/items/"+url.PathEscape(id), nil, &it); err != nil {
			return nil, out, err
		}
		out.Item = &it
		return nil, out, nil
	case "show":
		if id == "" {
			return nil, out, &refusedError{"show needs id: the backlog id, as at the top of your brief"}
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
			return nil, out, &refusedError{"unblock needs id and answer"}
		}
		return t.act(ctx, req, id, "unblock", in.Answer, "unblock typed to the worker. the item is running again once its turn starts from it")
	case "submit":
		if strings.TrimSpace(in.Pool) == "" || strings.TrimSpace(in.Brief) == "" {
			return nil, out, &refusedError{"submit needs pool and brief"}
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
			return nil, out, &refusedError{"cancel needs id"}
		}
		return t.act(ctx, req, id, "cancel", "", "cancel asked of the worker's room")
	}
	return nil, out, &refusedError{fmt.Sprintf("action %q is not one of list, get, show, unblock, cancel, submit", in.Action)}
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
