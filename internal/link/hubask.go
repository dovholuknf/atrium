package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"

	"github.com/dovholuknf/atrium/internal/link/ctl"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// A room's atrium_backlog and atrium_reports, answered by the hub it is attached to. See
// docs/changes/r-room-workers-lack-hub-tools.md.
//
// The room's stdio control asks its own room, the room sends one `hub` relay over the link it already holds, and the
// hub asks its own board here, over loopback, exactly as its own control MCP does. Nothing new listens and the room
// holds no credential it did not have.
//
// ONLY THESE ENDPOINTS, and not a general proxy: the path must be /_hub/backlog, /_hub/reports, /_hub/items or
// /_hub/merge (less the merge room and the land, which are the operator's) and what hangs off them. `by` and `room`
// on a write are set here from the link, so a room cannot file an item as another room.

// hubAskPath is whether a relayed path is one the room may ask. /_hub/backlog/follow is the hub's own card-to-item
// bookkeeping and is left to the control MCP.
func hubAskPath(p string) bool {
	if strings.Contains(p, "..") || strings.Contains(p, "\\") {
		return false
	}
	base, _, _ := strings.Cut(p, "?")
	// The board mux cleans the path and redirects, so an encoded dot-dot or a doubled slash could leave /_hub/backlog and
	// /_hub/reports: only a path that is already clean and has nothing encoded is passed on.
	if strings.ContainsAny(base, "%") || strings.Contains(base, "//") || path.Clean(base) != base {
		return false
	}
	if base == "/_hub/backlog/follow" || base == "/_hub/backlog/follow/" {
		return false
	}
	if base == hubPublishPath || base == hubSubsPath {
		return true
	}
	if base == "/_hub/merge" || strings.HasPrefix(base, "/_hub/merge/") {
		return mergeAskPath(strings.TrimPrefix(base, "/_hub/merge"))
	}
	for _, root := range []string{"/_hub/backlog", "/_hub/reports", "/_hub/items"} {
		if base == root || strings.HasPrefix(base, root+"/") {
			return true
		}
	}
	return false
}

// mergeAskPath is whether a room may ask the merge route under /_hub/merge: the queue, the verdict, one request and
// its cancel. Anything else there is the operator's.
func mergeAskPath(rest string) bool {
	switch rest {
	case "", "/report":
		return true
	}
	n, _ := strings.CutSuffix(strings.TrimPrefix(rest, "/"), "/cancel")
	_, ok := mergeN(n)
	return ok
}

// hubAsk answers one `hub` relay from the room `from`.
func (c *controlMCP) hubAsk(ctx context.Context, from string, req wire.RelayRequest) RelayAnswer {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method != http.MethodGet && method != http.MethodPost {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "the hub relays GET and POST only"}
	}
	if !hubAskPath(req.Path) {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "the hub relays the backlog, reports, items and merge endpoints only"}
	}
	if base, _, _ := strings.Cut(req.Path, "?"); base == hubPublishPath {
		return c.publishAsk(ctx, from, method, req)
	}
	var body any
	if method == http.MethodPost {
		m := map[string]any{}
		if len(req.Body) > 0 {
			if err := json.Unmarshal(req.Body, &m); err != nil {
				return RelayAnswer{Code: http.StatusBadRequest, Error: "the body is not a JSON object"}
			}
		}
		if m == nil {
			m = map[string]any{}
		}
		// FROM THE LINK, never from the room's word. See above.
		// `room` is only a field of a new row, and the endpoints refuse a field they do not know.
		if base, _, _ := strings.Cut(req.Path, "?"); base == "/_hub/backlog" || base == "/_hub/reports" ||
			base == "/_hub/items/sync" || base == "/_hub/items/tasks/sync" || base == hubSubsPath ||
			base == "/_hub/merge" || base == "/_hub/merge/report" {
			m["room"] = from
		} else if base == "/_hub/items" || (strings.HasPrefix(base, "/_hub/items/") && strings.HasSuffix(base, "/tasks")) {
			// A submit names the room its POOL is on, which is the caller's to say. `by` is still the link's.
		} else {
			delete(m, "room")
		}
		if sender := strings.TrimSpace(req.From); sender != "" {
			m["by"] = sender + "@" + from
		} else {
			delete(m, "by")
			if strings.HasPrefix(req.Path, "/_hub/merge") {
				m["by"] = "@" + from
			}
		}
		body = m
	}
	var data json.RawMessage
	if err := c.ask(ctx, method, req.Path, "", body, &data); err != nil {
		var be *ctl.BoardError
		if errors.As(err, &be) {
			return RelayAnswer{Code: be.Code, Error: be.Msg, Unreachable: be.Code == http.StatusBadGateway}
		}
		return RelayAnswer{Code: http.StatusBadGateway, Error: err.Error()}
	}
	return RelayAnswer{OK: true, Data: data}
}

// publishAsk answers a room's atrium_publish. FROM THE LINK: the room is the one that attached and the card is the
// sender it names on that room, which is the trust every other relayed write already has.
func (c *controlMCP) publishAsk(ctx context.Context, from, method string, req wire.RelayRequest) RelayAnswer {
	var in publishInput
	if method != http.MethodPost || json.Unmarshal(req.Body, &in) != nil {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "a publish is a POST of a JSON object"}
	}
	agent := strings.TrimSpace(req.From)
	_, out, err := c.publishFor(ctx, agent, from, agent+"@"+from, in)
	if err != nil {
		var dr *docsRefusal
		if errors.As(err, &dr) {
			return RelayAnswer{Code: dr.Status, Error: err.Error()}
		}
		return RelayAnswer{Code: http.StatusBadRequest, Error: err.Error()}
	}
	data, _ := json.Marshal(out)
	return RelayAnswer{OK: true, Data: data}
}
