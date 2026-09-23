package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"time"
)

// The terminal width floor, saved from the ALL view.
//
// The floor is a room setting: each room enforces it on its own runners. In the
// ALL view there is no room for the write to land in, so the hub passes it to
// every attached room. A room that refuses it (out of range) is relayed as the
// answer, so the board shows the room's own reason.

// minColsIn reports whether a settings write names ONLY `terminal_min_cols`,
// and puts the body back for whoever reads it next.
func minColsIn(r *http.Request) (payload []byte, found bool) {
	if r.URL.Path != "/v1/settings" || (r.Method != http.MethodPost && r.Method != http.MethodPut) {
		return nil, false
	}
	payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
	r.Body.Close()
	r.Body = io.NopCloser(bytes.NewReader(payload))
	if err != nil {
		return nil, false
	}
	var keys map[string]json.RawMessage
	if json.Unmarshal(payload, &keys) != nil || len(keys) != 1 {
		return nil, false
	}
	_, found = keys["terminal_min_cols"]
	return payload, found
}

// fanMinCols passes the floor to every attached room and answers with the last
// room's settings, or with the first refusal.
func (p *Proxy) fanMinCols(w http.ResponseWriter, r *http.Request, payload []byte) {
	var last []byte
	for _, room := range p.hub.Rooms() {
		code, body, err := p.postRoomSettings(r.Context(), room.Name, payload)
		if err != nil {
			writeErrBody(w, http.StatusBadGateway, room.Name+": "+err.Error())
			return
		}
		if code != http.StatusOK {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(code)
			_, _ = w.Write(body)
			return
		}
		last = body
	}
	if last == nil {
		writeErrBody(w, http.StatusConflict, "no room is attached to take the terminal width floor")
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write(last)
}

func (p *Proxy) postRoomSettings(ctx context.Context, room string, payload []byte) (int, []byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"http://"+hostFor(room)+"/v1/settings", bytes.NewReader(payload))
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	return res.StatusCode, body, err
}
