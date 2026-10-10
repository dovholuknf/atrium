package link

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"path"
	"strings"
)

// A room's atrium_backlog and atrium_reports, answered by the hub it is attached to. See
// docs/changes/r-room-workers-lack-hub-tools.md.
//
// The room's stdio control asks its own room, the room sends one `hub` relay over the link it already holds, and the
// hub asks its own board here, over loopback, exactly as its own control MCP does. Nothing new listens and the room
// holds no credential it did not have.
//
// ONLY THE TWO ENDPOINTS, and not a general proxy: the path must be /_hub/backlog or /_hub/reports and what hangs off
// them. `by` and `room` on a write are set here from the link, so a room cannot file an item as another room.

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
	for _, root := range []string{"/_hub/backlog", "/_hub/reports"} {
		if base == root || strings.HasPrefix(base, root+"/") {
			return true
		}
	}
	return false
}

// hubAsk answers one `hub` relay from the room `from`.
func (c *controlMCP) hubAsk(ctx context.Context, from string, req RelayRequest) RelayAnswer {
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	if method != http.MethodGet && method != http.MethodPost {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "the hub relays GET and POST only"}
	}
	if !hubAskPath(req.Path) {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "the hub relays the backlog and reports endpoints only"}
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
		if base, _, _ := strings.Cut(req.Path, "?"); base == "/_hub/backlog" || base == "/_hub/reports" {
			m["room"] = from
		} else {
			delete(m, "room")
		}
		if sender := strings.TrimSpace(req.From); sender != "" {
			m["by"] = sender + "@" + from
		} else {
			delete(m, "by")
		}
		body = m
	}
	var data json.RawMessage
	if err := c.ask(ctx, method, req.Path, "", body, &data); err != nil {
		var be *boardError
		if errors.As(err, &be) {
			return RelayAnswer{Code: be.code, Error: be.msg, Unreachable: be.code == http.StatusBadGateway}
		}
		return RelayAnswer{Code: http.StatusBadGateway, Error: err.Error()}
	}
	return RelayAnswer{OK: true, Data: data}
}
