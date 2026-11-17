package link

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/edge"
	"github.com/dovholuknf/atrium/internal/link/wire"
)

// A ROOM'S EVENT LOG, TAKEN BY THE HUB. A room that could not reach the hub holds a backlog write or a merge ask as
// a numbered row and sends it when the link is back, as `POST /_hub/room-events/apply` over the `hub` relay. The hub
// checks (room, seq) against what it took, applies the held request as the relay would have, and records the key, so
// a row sent twice is applied once. The room comes from the link. See docs/design/hub-offline-tolerance.md.
//
//	GET  /_hub/room-events/apply        {high}: the highest seq the hub took of the asking room. The room resends after it
//	GET  /_hub/room-events?room=&seq=   {seen}, or {high} with &high=1. The hub's own, loopback only, not a relayed path
//	POST /_hub/room-events              {room, seq, kind, key, applied}: record a row as taken. Loopback only

const hubRoomEventsPath = "/_hub/room-events/apply"

type roomEventIn struct {
	Seq    int64           `json:"seq"`
	Kind   string          `json:"kind"`
	Key    string          `json:"key"`
	Method string          `json:"method"`
	Path   string          `json:"path"`
	Body   json.RawMessage `json:"body"`
	// Replay is a row the room already delivered once, to a hub that was here before this one. A filing or an edit is
	// applied again, as an upsert for the filing. A merge ask is history: it is taken and not asked again.
	Replay bool `json:"replay"`
}

// roomEventAllowed is whether a held row carries the request its kind names. A room cannot use the log to reach
// anything the relay would not carry for it.
func roomEventAllowed(kind, method, p string) bool {
	if method != http.MethodPost || !hubAskPath(p) {
		return false
	}
	base, _, _ := strings.Cut(p, "?")
	switch kind {
	case "backlog.file":
		return base == "/_hub/backlog"
	case "backlog.edit":
		return strings.HasPrefix(base, "/_hub/backlog/") && strings.Count(base, "/") == 3
	case "merge.ask":
		return base == "/_hub/merge"
	}
	return false
}

func (c *controlMCP) roomEventAsk(ctx context.Context, from string, req wire.RelayRequest) RelayAnswer {
	if strings.EqualFold(req.Method, http.MethodGet) {
		var high json.RawMessage
		if err := c.Ask(ctx, http.MethodGet, "/_hub/room-events?high=1&room="+from, "", nil, &high); err != nil {
			return RelayAnswer{Code: http.StatusBadGateway, Error: err.Error()}
		}
		return RelayAnswer{OK: true, Data: high}
	}
	var in roomEventIn
	if json.Unmarshal(req.Body, &in) != nil || in.Seq < 1 || !roomEventAllowed(in.Kind, strings.ToUpper(in.Method), in.Path) {
		return RelayAnswer{Code: http.StatusBadRequest, Error: "a held event is a numbered POST the relay carries"}
	}
	var seen struct {
		Seen bool `json:"seen"`
	}
	q := "/_hub/room-events?room=" + from + "&seq=" + strconv.FormatInt(in.Seq, 10)
	if err := c.Ask(ctx, http.MethodGet, q, "", nil, &seen); err != nil {
		return RelayAnswer{Code: http.StatusBadGateway, Error: err.Error()}
	}
	if seen.Seen {
		data, _ := json.Marshal(map[string]any{"applied": false, "seen": true})
		return RelayAnswer{OK: true, Data: data}
	}
	if in.Replay && in.Kind == "merge.ask" {
		rec := map[string]any{"room": from, "seq": in.Seq, "kind": in.Kind, "key": in.Key, "applied": false}
		if err := c.Ask(ctx, http.MethodPost, "/_hub/room-events", "", rec, nil); err != nil {
			return RelayAnswer{Code: http.StatusBadGateway, Error: err.Error()}
		}
		data, _ := json.Marshal(map[string]any{"applied": false, "seen": false, "history": true})
		return RelayAnswer{OK: true, Data: data}
	}
	inner := c.hubAsk(ctx, from, wire.RelayRequest{Op: req.Op, From: req.From, Method: http.MethodPost, Path: in.Path,
		Body: in.Body})
	refused := !inner.OK && inner.Code >= 400 && inner.Code < 500
	if !inner.OK && !refused {
		return inner
	}
	rec := map[string]any{"room": from, "seq": in.Seq, "kind": in.Kind, "key": in.Key, "applied": inner.OK}
	if err := c.Ask(ctx, http.MethodPost, "/_hub/room-events", "", rec, nil); err != nil {
		return RelayAnswer{Code: http.StatusBadGateway, Error: err.Error()}
	}
	data, _ := json.Marshal(map[string]any{"applied": inner.OK, "seen": false, "code": inner.Code, "error": inner.Error,
		"data": inner.Data})
	return RelayAnswer{OK: true, Data: data}
}

func (p *Proxy) serveRoomEvents(w http.ResponseWriter, r *http.Request) {
	st := p.itemStore()
	if st == nil {
		http.NotFound(w, r)
		return
	}
	if !edge.LocalOperator(r) {
		crFail(w, http.StatusForbidden, "room events are taken only from the machine the hub runs on"+edge.ProxyNote(r))
		return
	}
	switch r.Method {
	case http.MethodGet:
		if r.URL.Query().Get("high") != "" {
			room := r.URL.Query().Get("room")
			if !validRoomName(room) {
				crFail(w, http.StatusBadRequest, "a room")
				return
			}
			high, err := st.RoomEventHigh(room)
			if err != nil {
				crFail(w, http.StatusInternalServerError, err.Error())
				return
			}
			crJSON(w, http.StatusOK, map[string]any{"high": high})
			return
		}
		seq, _ := strconv.ParseInt(r.URL.Query().Get("seq"), 10, 64)
		room := r.URL.Query().Get("room")
		if !validRoomName(room) || seq < 1 {
			crFail(w, http.StatusBadRequest, "a room and a seq")
			return
		}
		seen, err := st.RoomEventSeen(room, seq)
		if err != nil {
			crFail(w, http.StatusInternalServerError, err.Error())
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"seen": seen})
	case http.MethodPost:
		var in struct {
			Room    string `json:"room"`
			Seq     int64  `json:"seq"`
			Kind    string `json:"kind"`
			Key     string `json:"key"`
			Applied bool   `json:"applied"`
		}
		if err := crDecode(r, &in); err != nil || !validRoomName(in.Room) || in.Seq < 1 {
			crFail(w, http.StatusBadRequest, "a room, a seq and a kind")
			return
		}
		if err := st.RoomEventTake(in.Room, in.Seq, in.Kind, in.Key, in.Applied); err != nil {
			crFail(w, http.StatusInternalServerError, err.Error())
			return
		}
		crJSON(w, http.StatusOK, map[string]any{"ok": true})
	default:
		crFail(w, http.StatusMethodNotAllowed, "that has to be a GET or a POST")
	}
}
