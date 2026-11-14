package link

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// A hook of a session started by hand, carried to the room that holds the card for its conversation.
//
// The hub holds no card and no state here. With no room named it asks every other attached room, and the room that
// holds the conversation takes the hook and answers. With a room and a card it carries the hook there. The owning room
// runs its own handlers, so the permission chain is its own and unchanged. `from` comes from the link, so a room cannot
// ask as another.

// handHookPath is the room's board route that takes a carried hook.
const handHookPath = "/v1/hand/hook"

func (c *controlMCP) handAcross(ctx context.Context, from string, req wire.RelayRequest) RelayAnswer {
	if req.Path == "" || len(req.Body) == 0 {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "a hand relay carries a hook path and its body"}
	}
	body := map[string]any{"path": req.Path, "card": strings.TrimSpace(req.To), "body": req.Body}
	if room := strings.TrimSpace(req.Room); room != "" {
		if wire.EqualFold(room, from) {
			return RelayAnswer{Code: http.StatusBadRequest, Error: "the relay carries a hook to another room"}
		}
		if ans, ok := c.reachable(ctx, room); !ok {
			return ans
		}
		var out json.RawMessage
		if err := c.Ask(ctx, http.MethodPost, handHookPath, room, body, &out); err != nil {
			return refusal(err)
		}
		return RelayAnswer{OK: true, Data: out}
	}
	if c.hub == nil {
		return RelayAnswer{Code: http.StatusNotFound, Error: "no room holds that conversation"}
	}
	for _, a := range c.hub.Rooms() {
		if wire.EqualFold(a.Name, from) {
			continue
		}
		var out map[string]any
		if err := c.Ask(ctx, http.MethodPost, handHookPath, a.Name, body, &out); err != nil {
			continue
		}
		out["room"] = a.Name
		data, _ := json.Marshal(out)
		return RelayAnswer{OK: true, Data: data}
	}
	return RelayAnswer{Code: http.StatusNotFound, Error: "no room holds that conversation"}
}
