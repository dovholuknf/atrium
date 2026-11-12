package cli

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_remind on the stdio server. The hub keeps the reminder and raises a growler when it is due, so it survives a
// restart and a parked card. The call goes to this room, which asks the hub over its link. See link/remind.go.

const remindWait = 30 * time.Second

const remindToolDesc = "Ask atrium to bring something back to you, or to another card on this room, later. When it is " +
	"due the hub raises an alert on the board and the phone with your text, which the operator can snooze. It is kept " +
	"on the hub, so it survives restarts and a parked card. At most 90 days out.\n\n" +
	"- `set`: `in` (such as `3d`, `4h`, `90m`) or `at` (RFC3339), and `text`. Optional `card` (a card id on this " +
	"room, default yours). `wake` also types the text into the card when it is due, resuming it if parked.\n" +
	"- `list`: the pending reminders on the card.\n" +
	"- `cancel`: the `id` of one, or every pending one on the card when `id` is left out."

// RemindInput is the atrium_remind tool's input.
type RemindInput struct {
	Action string `json:"action,omitempty" jsonschema:"set, list or cancel. default set when text is given, else list"`
	In     string `json:"in,omitempty" jsonschema:"set: how long from now, such as 3d, 4h or 90m"`
	At     string `json:"at,omitempty" jsonschema:"set: an RFC3339 time, instead of in"`
	Text   string `json:"text,omitempty" jsonschema:"set: what to remind about"`
	Card   string `json:"card,omitempty" jsonschema:"a card id on this room. default your own card"`
	Wake   bool   `json:"wake,omitempty" jsonschema:"set: type the text into the card when due, resuming it if parked"`
	ID     string `json:"id,omitempty" jsonschema:"cancel: the id of one reminder, from list"`
}

func remindHandler(ctx context.Context, _ *mcp.CallToolRequest, in RemindInput) (*mcp.CallToolResult, map[string]any, error) {
	out := map[string]any{}
	switch a := strings.TrimSpace(in.Action); a {
	case "":
		in.Action = "list"
		if strings.TrimSpace(in.Text) != "" {
			in.Action = "set"
		}
	case "set", "list", "cancel":
	default:
		return nil, out, fmt.Errorf("action is set, list or cancel")
	}
	path, err := myCardPath("/remind")
	if err != nil {
		return nil, out, err
	}
	err = askFor(ctx, remindWait, http.MethodPost, path, in, &out)
	if olderRoom(err) {
		return nil, out, fmt.Errorf("this room predates reminders. update the room")
	}
	return nil, out, err
}

func addRemindTool(s *mcp.Server) {
	mcp.AddTool(s, &mcp.Tool{Name: "atrium_remind", Description: remindToolDesc}, remindHandler)
}
