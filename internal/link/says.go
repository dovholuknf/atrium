package link

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/url"
	"sort"
	"sync"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

// `GET /v1/says` across every room: what every session said to another, newest first.
//
// ── why this is not a row in `merged` ────────────────────
//
// It is paged, and ids are not comparable across rooms, so a page is cut by `sent_at` and the cursor has to say where
// each room is. It is the same reason `history` is its own function, with a different answer: history re-reads every
// room from the top to the end of the page, which is fine for a few hundred rows of cards and not for 16 KB of text a
// say. Here each room is asked for ONE page from its own position, the page is cut from the merge, and the cursor
// moves each room on by exactly the rows of its that were shown.
//
// ── the cursor ───────────────────────────────────────────
//
// Opaque to the board: base64 of `{room: id}`, where id is the last row of that room already shown and `-` means that
// room has nothing further back. A room not in it has not been read yet. A room that did not answer keeps the position
// it had, so the page after it comes back picks up where it was, and is named in `missing` meanwhile.

const saysPage = 20

// saysDone marks a room with nothing further back.
const saysDone = "-"

func encodeSaysCursor(pos map[string]string) string {
	if len(pos) == 0 {
		return ""
	}
	raw, _ := json.Marshal(pos)
	return base64.RawURLEncoding.EncodeToString(raw)
}

// decodeSaysCursor is the position per room, and false for a cursor that is not one of ours.
func decodeSaysCursor(c string) (map[string]string, bool) {
	pos := map[string]string{}
	if c == "" {
		return pos, true
	}
	raw, err := base64.RawURLEncoding.DecodeString(c)
	if err != nil || json.Unmarshal(raw, &pos) != nil {
		return nil, false
	}
	return pos, true
}

func (p *Proxy) says(w http.ResponseWriter, r *http.Request) {
	rooms := p.hub.Rooms()
	if len(rooms) == 0 {
		writeErrBody(w, http.StatusServiceUnavailable, "no room is attached")
		return
	}
	q := r.URL.Query()
	pos, ok := decodeSaysCursor(q.Get("before"))
	if !ok {
		writeErrBody(w, http.StatusBadRequest, "that cursor is not one this hub gave out")
		return
	}

	// `room` narrows the rooms asked. `agent`, `from` and `to` may be a card id tagged with its room, which names one room as
	// well and is handed to it bare.
	only := q.Get("room")
	filters := url.Values{}
	for _, k := range []string{"state", "since"} {
		if v := q.Get(k); v != "" {
			filters.Set(k, v)
		}
	}
	for _, k := range []string{"agent", "from", "to"} {
		v := q.Get(k)
		if v == "" {
			continue
		}
		if room, bare := wire.SplitTag(v); room != "" {
			if only != "" && wire.KeyOf(only) != wire.KeyOf(room) {
				only = "\x00" // two different rooms named: nothing can match
			} else {
				only = room
			}
			v = bare
		}
		filters.Set(k, v)
	}

	type answer struct {
		room string
		rows []map[string]any
		more bool
		ok   bool
	}
	var asked []string
	for _, room := range rooms {
		if only != "" && wire.KeyOf(room.Name) != wire.KeyOf(only) {
			continue
		}
		if pos[room.Name] == saysDone {
			continue
		}
		asked = append(asked, room.Name)
	}
	out := make([]answer, len(asked))
	var wg sync.WaitGroup
	for i, name := range asked {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			out[i].room = name
			vals := url.Values{}
			for k, v := range filters {
				vals[k] = v
			}
			if at := pos[name]; at != "" {
				vals.Set("before", at)
			}
			ask := *r.URL
			ask.Path, ask.RawPath, ask.RawQuery = "/v1/says", "", vals.Encode()
			scan := r.Clone(r.Context())
			scan.URL = &ask
			body := p.askRoomBody(scan, name)
			if body == nil {
				return
			}
			out[i].ok = true
			rows, _ := body["says"].([]any)
			for _, row := range rows {
				if obj, ok := row.(map[string]any); ok {
					out[i].rows = append(out[i].rows, obj)
				}
			}
			out[i].more, _ = body["more"].(bool)
		}(i, name)
	}
	wg.Wait()

	type row struct {
		room string
		obj  map[string]any
	}
	var all []row
	var missing []string
	for _, a := range out {
		if !a.ok {
			missing = append(missing, a.room)
			continue
		}
		for _, obj := range a.rows {
			all = append(all, row{a.room, obj})
		}
	}
	// Newest first. Within one room this is the room's own order, so the rows shown from a room are always a prefix of
	// what it sent, which is what lets the cursor be one id per room.
	sort.SliceStable(all, func(i, j int) bool {
		ai, _ := all[i].obj["sent_at"].(string)
		aj, _ := all[j].obj["sent_at"].(string)
		if ai != aj {
			return ai > aj
		}
		if all[i].room != all[j].room {
			return all[i].room < all[j].room
		}
		bi, _ := all[i].obj["id"].(string)
		bj, _ := all[j].obj["id"].(string)
		return bi > bj
	})
	if len(all) > saysPage {
		all = all[:saysPage]
	}

	next := map[string]string{}
	for k, v := range pos {
		next[k] = v
	}
	shown := map[string]int{}
	for _, rw := range all {
		id, _ := rw.obj["id"].(string)
		next[rw.room] = id
		shown[rw.room]++
		rw.obj["room"] = rw.room
		for _, k := range []string{"from_task", "to_task"} {
			if v, ok := rw.obj[k].(string); ok {
				rw.obj[k] = wire.TagFor(rw.room, v)
			}
		}
		// The row's own id is the room's, so two rooms can mint the same one. Tagged like a card's.
		rw.obj["id"] = wire.TagFor(rw.room, id)
	}
	more := false
	for _, a := range out {
		if !a.ok {
			continue
		}
		if shown[a.room] == len(a.rows) && !a.more {
			next[a.room] = saysDone
			continue
		}
		more = true
	}

	page := make([]map[string]any, 0, len(all))
	for _, rw := range all {
		page = append(page, rw.obj)
	}
	body := map[string]any{"says": page, "more": more}
	if more {
		body["cursor"] = encodeSaysCursor(next)
	}
	if len(missing) > 0 {
		sort.Strings(missing)
		body["missing"] = missing
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
}
