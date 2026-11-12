package link

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/link/ctl"
)

const remindToolDesc = "Ask atrium to bring something back to you, or to another card on this room, later. When it is " +
	"due the hub raises an alert on the board and the phone with your text, which the operator can snooze. It is kept " +
	"on the hub, so it survives restarts and a parked card. At most 90 days out.\n\n" +
	"- `set`: `in` (such as `3d`, `4h`, `90m`) or `at` (RFC3339), and `text`. Optional `card` (a card id on this " +
	"room, default yours). `wake` also types the text into the card when it is due, resuming it if parked.\n" +
	"- `list`: the pending reminders on the card.\n" +
	"- `cancel`: the `id` of one, or every pending one on the card when `id` is left out."

type remindInput struct {
	Action string `json:"action,omitempty" jsonschema:"set, list or cancel. default set when text is given, else list"`
	In     string `json:"in,omitempty" jsonschema:"set: how long from now, such as 3d, 4h or 90m"`
	At     string `json:"at,omitempty" jsonschema:"set: an RFC3339 time, instead of in"`
	Text   string `json:"text,omitempty" jsonschema:"set: what to remind about"`
	Card   string `json:"card,omitempty" jsonschema:"a card id on this room. default your own card"`
	Wake   bool   `json:"wake,omitempty" jsonschema:"set: type the text into the card when due, resuming it if parked"`
	ID     string `json:"id,omitempty" jsonschema:"cancel: the id of one reminder, from list"`
}

// remindHandler forwards to the calling card's room, which names the card and asks the hub. See api.cardRemind.
func (c *controlMCP) remindHandler(ctx context.Context, req *mcp.CallToolRequest, in remindInput) (
	*mcp.CallToolResult, map[string]any, error) {

	out := map[string]any{}
	room, me := ctl.RoomOf(req), ctl.AgentOf(req)
	if me == "" {
		return nil, out, fmt.Errorf("there is no session calling this to remind")
	}
	switch strings.TrimSpace(in.Action) {
	case "":
		in.Action = "list"
		if strings.TrimSpace(in.Text) != "" {
			in.Action = "set"
		}
	case "set", "list", "cancel":
	default:
		return nil, out, fmt.Errorf("action is set, list or cancel")
	}
	scope, id, _, err := c.resolveCardStrict(ctx, room, me)
	if err != nil {
		return nil, out, err
	}
	err = c.Ask(ctx, http.MethodPost, "/v1/tasks/"+url.PathEscape(id)+"/remind", scope, in, &out)
	return nil, out, err
}
