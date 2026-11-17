package hublink

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

const roomEventsApply = "/_hub/room-events/apply"

// heldKind is the room event kind of a hub write this room logs, "" for every other call. A read is never logged, and
// neither is a call that only names a path: only a filing, an edit and a merge ask.
func heldKind(method, path string) string {
	if method != http.MethodPost || strings.Contains(path, "?") {
		return ""
	}
	switch {
	case path == "/_hub/backlog":
		return store.RoomEventBacklogFile
	case strings.HasPrefix(path, "/_hub/backlog/") && strings.Count(path, "/") == 3:
		return store.RoomEventBacklogEdit
	case path == "/_hub/merge":
		return store.RoomEventMergeAsk
	}
	return ""
}

// askLogged writes a hub write into this room's event log, then sends it as that row. The room owns the log: the hub
// takes the row once by (room, seq), and a hub that has never seen this room is caught up from the same rows. It says
// false for a call it does not log, and the caller asks the hub as before. THE ANSWER WHILE THE HUB IS AWAY SAYS HELD,
// NOT DONE (202): nothing is filed, queued or merged until the hub takes the row.
func (d *Service) askLogged(ctx context.Context, from, method, path string, body json.RawMessage) (int, []byte, bool) {
	kind := heldKind(method, path)
	ha, _ := d.Relay().(HubAsker)
	if kind == "" || d.St == nil || ha == nil {
		return 0, nil, false
	}
	obj := map[string]any{}
	if len(body) > 0 && json.Unmarshal(body, &obj) != nil {
		return 0, nil, false
	}
	if kind == store.RoomEventBacklogFile {
		if s, _ := obj["dept"].(string); strings.TrimSpace(s) == "" {
			return 0, nil, false
		}
		if s, _ := obj["title"].(string); strings.TrimSpace(s) == "" {
			return 0, nil, false
		}
	}
	e := store.RoomEvent{Kind: kind, Sender: from, Method: method, Path: path, Body: string(body)}
	if kind == store.RoomEventBacklogEdit {
		e.Key = strings.TrimPrefix(path, "/_hub/backlog/")
	}
	seq, err := d.St.RoomEventAppend(e)
	if err != nil {
		log.Printf("[atrium] could not log a hub write: %v", err)
		return 0, nil, false
	}
	minted := ""
	if id, _ := obj["id"].(string); kind == store.RoomEventBacklogFile && id == "" && d.RoomName != nil && d.RoomName() != "" {
		minted = strings.ToLower(d.RoomName()) + "-" + strconv.FormatInt(seq, 10)
		obj["id"], obj["upsert"] = minted, true
		raw, _ := json.Marshal(obj)
		e.Key, e.Body = minted, string(raw)
		if err := d.St.RoomEventSetBody(seq, e.Key, e.Body); err != nil {
			log.Printf("[atrium] could not log a hub write: %v", err)
			return 0, nil, false
		}
	}
	req, _ := json.Marshal(map[string]any{"seq": seq, "kind": kind, "key": e.Key, "method": method, "path": path,
		"body": json.RawMessage(e.Body)})
	cctx, cancel := context.WithTimeout(ctx, RelayWait)
	defer cancel()
	res, err := ha.Hub(cctx, from, http.MethodPost, roomEventsApply, req)
	switch {
	case errors.Is(err, ErrRelayDown) || err == nil && !res.OK && res.Unreachable:
		return d.holdLogged(seq, kind, minted)
	case errors.Is(err, ErrRelayUnconfirmed):
		return http.StatusGatewayTimeout, errBody(err.Error() + ". it is held and sent again, so do not repeat it"), true
	case err != nil:
		_ = d.St.RoomEventSent(seq, "not sent: "+err.Error())
		return 0, nil, false
	case !res.OK && (strings.Contains(res.Error, "does not know the relay op") || strings.Contains(res.Error, "relays the backlog")):
		_ = d.St.RoomEventSent(seq, "not sent: the hub predates the room event log")
		return 0, nil, false
	case !res.OK:
		_ = d.St.RoomEventSent(seq, "the hub refused it: "+res.Error)
		return codeOr(res.Code), errBody(res.Error), true
	}
	var got struct {
		Applied bool            `json:"applied"`
		Code    int             `json:"code"`
		Error   string          `json:"error"`
		Data    json.RawMessage `json:"data"`
	}
	_ = json.Unmarshal(res.Data, &got)
	if !got.Applied && got.Error != "" {
		_ = d.St.RoomEventSent(seq, "the hub refused it: "+got.Error)
		return codeOr(got.Code), errBody(got.Error), true
	}
	_ = d.St.RoomEventSent(seq, "")
	if kind == store.RoomEventBacklogFile && minted == "" {
		var item struct {
			ID string `json:"id"`
		}
		if json.Unmarshal(got.Data, &item) == nil && item.ID != "" {
			obj["id"], obj["upsert"] = item.ID, true
			raw, _ := json.Marshal(obj)
			_ = d.St.RoomEventSetBody(seq, item.ID, string(raw))
		}
	}
	if minted != "" {
		var wrapped struct {
			Item json.RawMessage `json:"item"`
		}
		if json.Unmarshal(got.Data, &wrapped) == nil && len(wrapped.Item) > 0 {
			got.Data = wrapped.Item
		}
	}
	if len(got.Data) == 0 {
		got.Data = []byte("{}")
	}
	return http.StatusOK, got.Data, true
}

// holdLogged answers a write the hub did not take. A filing carries the id this room minted, so a replay anywhere is the
// same item.
func (d *Service) holdLogged(seq int64, kind, id string) (int, []byte, bool) {
	out := map[string]any{"held": true, "seq": seq, "kind": kind, "queued": false, "done": false}
	verb := map[string]string{store.RoomEventMergeAsk: "queued", store.RoomEventBacklogFile: "filed",
		store.RoomEventBacklogEdit: "changed"}[kind]
	if id != "" {
		out["id"] = id
	}
	held, _, _ := d.St.RoomEventsHeld()
	out["held_count"] = held
	out["answer"] = "held, not " + verb + ". the hub is not answering, so this room keeps the request and sends it when " +
		"the hub is back. nothing has happened yet. do not repeat it"
	raw, _ := json.Marshal(out)
	if d.KickItems != nil {
		d.KickItems()
	}
	return http.StatusAccepted, raw, true
}

func errBody(msg string) []byte {
	raw, _ := json.Marshal(ErrBody(msg))
	return raw
}

func codeOr(c int) int {
	if c < 400 {
		return http.StatusBadGateway
	}
	return c
}
