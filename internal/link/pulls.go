package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// THE PULLS VIEW THROUGH THE HUB. Design: docs/rnd/pulls-view-design.md section 9. Contract: docs/rnd/pulls-api.md.
//
// Every room serves `/v1/prs...` on its human listener, and the hub's job is small because most of it is the byte pipe
// it already is. With one room addressed (a header, a scoped board, a single attached room) nothing here runs: the
// request goes to the room untouched, with its method, query, body, `If-None-Match`, `ETag`, `304` and error bodies.
// There is no body cap, response cap or retry on that path, and the only clock is the read wait every GET has, which
// the findings fit inside.
//
// What this file adds is the ALL view, where two or more rooms are attached and none is named:
//
//   - `GET /v1/prs` is merged. See pullsList. It is the one pulls route that is, and it is not a row in `merged`,
//     because it carries counts and a sort that table cannot say.
//   - `POST /v1/prs` is not merged and not guessed. It falls to `needsARoom`, the same question `POST /v1/tasks` asks.
//   - `/v1/prs/<id>...` names one row. A tagged `room~pr_...` goes to that room with the bare id, and the answer's
//     `pr` is tagged on the way back. A plain id with two or more rooms is looked for, bounded, and remembered for a
//     while. A per-PR route is never merged, so it is not in `merged` and must not be added to it.
//
// FINDINGS ARE NEVER PARSED BEYOND THE `pr` OBJECT. The `hash` a write quotes back is the room's hash of the file, and
// a body that went through a map and back would change bytes (an escaped `<`, a reordered key) without changing what
// anyone reads, which is exactly the difference a hash precondition exists to catch. So the answer is rewritten as raw
// JSON values, and only `pr.id`, `pr.walker_task` and `pr.room` are replaced. See retagPR.

// prIDIn pulls the row id out of `/v1/prs/<id>/...`, which is the only shape that carries one.
func prIDIn(path string) string {
	const pre = "/v1/prs/"
	if !strings.HasPrefix(path, pre) {
		return ""
	}
	rest := strings.TrimPrefix(path, pre)
	if i := strings.IndexByte(rest, '/'); i >= 0 {
		rest = rest[:i]
	}
	return rest
}

// placePR routes a request that names a row to the room that holds it. The pulls twin of `placeCard`.
//
//  1. `room~pr_...` names the room outright. The answer is tagged on the way back, as a tagged card's is.
//  2. A room named in a header or a query is trusted. A plain id only reaches a board that is scoped to one room,
//     because the ALL view's rows all carry a tag, so the header cannot be stale about it the way it can be about
//     a card.
//  3. Otherwise, with two or more rooms, the holder is looked for. See roomHoldingPR.
//
// It returns the request to carry on with, or false once it has answered.
func (p *Proxy) placePR(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	id := prIDIn(r.URL.Path)
	if id == "" {
		return r, true
	}
	if room, _ := splitTag(id); room != "" {
		r = r.WithContext(context.WithValue(r.Context(), taggedKey{}, room))
		return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, room)), true
	}
	if strings.TrimSpace(r.Header.Get(RoomHeader)) != "" || strings.TrimSpace(r.URL.Query().Get(RoomParam)) != "" {
		return r, true
	}
	rooms := p.hub.Rooms()
	if len(rooms) < 2 {
		// ONE ROOM IS NOTHING TO CHOOSE BETWEEN. The dial goes to it and it answers for itself.
		return r, true
	}
	if owner, ok := p.roomHoldingPR(r, id, rooms); ok {
		return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, owner)), true
	}
	names := make([]string, 0, len(rooms))
	for _, a := range rooms {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": "pr " + id + " was not found on room " + strings.Join(names, ", room "),
		"code":  "not_found", "rooms": names,
	})
	return r, false
}

// roomHoldingPR finds which attached room holds a bare row id, by asking each room for the row, bounded, and taking
// the first that says yes. The twin of `roomHolding`, and for the same reasons: a quiet room is skipped rather than
// failing the lookup, and the first yes wins because an id is unique. The answer is cached briefly, in the same
// table the cards use. A `pr_` id never collides with a card's.
func (p *Proxy) roomHoldingPR(r *http.Request, bare string, rooms []Attached) (string, bool) {
	if room, ok := p.cachedCardRoom(bare); ok {
		return room, true
	}
	type held struct {
		room string
		has  bool
	}
	results := make(chan held, len(rooms))
	for _, room := range rooms {
		go func(name string) { results <- held{room: name, has: p.roomHasPR(r.Context(), name, bare)} }(room.Name)
	}
	for range rooms {
		if h := <-results; h.has {
			p.rememberCardRoom(bare, h.room)
			return h.room, true
		}
	}
	return "", false
}

// roomHasPR asks one room for one row and reads the status and the id. A room on a build with no pulls answers 404,
// which is a no here and nothing more.
func (p *Proxy) roomHasPR(ctx context.Context, room, bare string) bool {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostFor(room)+"/v1/prs/"+bare, nil)
	if err != nil {
		return false
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	var body struct {
		PR struct {
			ID string `json:"id"`
		} `json:"pr"`
	}
	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&body)
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<20))
	return res.StatusCode == http.StatusOK && err == nil && body.PR.ID == bare
}

// prRetagLimit is the most a pulls answer is read to be re-tagged. A findings list is the big one. Past it the answer
// goes through whole and untagged, which is a worse answer than the room's own and not a wrong one.
const prRetagLimit = 16 << 20

// retagPR puts the room back on a tagged row's answer. The pulls twin of `retagCard`, and the other half of `untag`.
//
// ONLY FOR A REQUEST THAT ARRIVED TAGGED, and only a 2xx JSON answer. A scoped board asked a room directly and wants
// the room's own ids, and an error body names no row. `pr.id` and `pr.walker_task` (a card id) are tagged and `pr.room`
// is set. A walker answer's top-level `task` is a card id too and is tagged the same way. Everything else in the body
// is passed on as the raw JSON the room wrote.
func (p *Proxy) retagPR(res *http.Response) error {
	if res.Request == nil || res.StatusCode < 200 || res.StatusCode > 299 || res.StatusCode == http.StatusNoContent {
		return nil
	}
	room, _ := res.Request.Context().Value(taggedKey{}).(string)
	if room == "" || !strings.HasPrefix(res.Header.Get("Content-Type"), "application/json") {
		return nil
	}
	raw, err := io.ReadAll(io.LimitReader(res.Body, prRetagLimit+1))
	if err != nil {
		res.Body.Close()
		return err
	}
	if len(raw) > prRetagLimit {
		res.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(raw), res.Body), res.Body}
		return nil
	}
	res.Body.Close()
	keep := func() { res.Body = io.NopCloser(bytes.NewReader(raw)) }
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil || top == nil {
		keep()
		return nil
	}
	if row, ok := top["pr"]; ok {
		var fields map[string]json.RawMessage
		if json.Unmarshal(row, &fields) == nil && fields != nil {
			fields["room"] = rawString(room)
			for _, f := range []string{"id", "walker_task"} {
				fields[f] = tagRaw(room, fields[f])
			}
			top["pr"] = rawObject(fields)
		}
	}
	if t, ok := top["task"]; ok {
		top["task"] = tagRaw(room, t)
	}
	out := rawObject(top)
	if out == nil {
		keep()
		return nil
	}
	res.Body = io.NopCloser(bytes.NewReader(out))
	res.ContentLength = int64(len(out))
	res.Header.Set("Content-Length", fmt.Sprint(len(out)))
	return nil
}

// rawString is a JSON string value, with no HTML escaping, so a value the hub writes is as plain as the room's.
func rawString(s string) json.RawMessage { return rawEncode(s) }

// rawObject is a JSON object of raw values, nil when it could not be written. Keys come out sorted, and nothing the
// room wrote inside a value is touched, or HTML-escaped.
func rawObject(fields map[string]json.RawMessage) json.RawMessage { return rawEncode(fields) }

func rawEncode(v any) json.RawMessage {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if enc.Encode(v) != nil {
		return nil
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

// tagRaw tags a JSON string value `room~value`. Anything that is not a non-empty string is returned as it was, which
// is what keeps an empty `walker_task` empty.
func tagRaw(room string, v json.RawMessage) json.RawMessage {
	var s string
	if len(v) == 0 || json.Unmarshal(v, &s) != nil || s == "" {
		return v
	}
	return rawString(tagFor(room, s))
}

// ── the ALL view's list ──────────────────────────────────

// pullsList answers `GET /v1/prs` from every attached room.
//
// Rows get the room and a tagged `id`, and a tagged `walker_task` when there is one. `counts` is summed key by key and
// `nav_count` is summed. The rows are sorted newest first by `created_at` with the tagged id breaking ties, so a board
// that draws the answer as it comes is in the order a room would have drawn it. The query goes to every room as it was
// sent, `state` and `org_repo` and `archived` alike.
//
// A ROOM THAT DOES NOT ANSWER CONTRIBUTES NOTHING AND THE REST DRAWS, as `aggregate` does, and is listed in
// `rooms_quiet`. A room on a build with no pulls answers 404, which is an answer: it contributes nothing and is not
// quiet. It is named in `rooms_without` instead, so the board can say why a room is missing rather than guess.
//
// Returns false to fall through, which is what a hub with no room attached wants.
func (p *Proxy) pullsList(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v1/prs" || (r.Method != http.MethodGet && r.Method != http.MethodHead) {
		return false
	}
	rooms := p.hub.Rooms()
	if len(rooms) == 0 {
		return false
	}
	type answer struct {
		room    string
		body    map[string]any
		without bool
		err     error
	}
	out := make([]answer, len(rooms))
	var wg sync.WaitGroup
	for i, room := range rooms {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			out[i].room = name
			out[i].body, out[i].without, out[i].err = p.askPulls(r, name)
		}(i, room.Name)
	}
	wg.Wait()

	all := []map[string]any{}
	counts := map[string]float64{"queued": 0, "fetching": 0, "running": 0, "ready": 0, "failed": 0, "aborted": 0}
	nav := 0.0
	var quiet, without []string
	for _, a := range out {
		switch {
		case a.err != nil:
			quiet = append(quiet, a.room)
			continue
		case a.without:
			without = append(without, a.room)
			continue
		}
		rows, _ := a.body["prs"].([]any)
		for _, row := range rows {
			obj, ok := row.(map[string]any)
			if !ok {
				continue
			}
			obj["room"] = a.room
			if id, ok := obj["id"].(string); ok {
				obj["id"] = tagFor(a.room, id)
			}
			if id, ok := obj["walker_task"].(string); ok && id != "" {
				obj["walker_task"] = tagFor(a.room, id)
			}
			all = append(all, obj)
		}
		if c, ok := a.body["counts"].(map[string]any); ok {
			for k, v := range c {
				n, _ := v.(float64)
				counts[k] += n
			}
		}
		n, _ := a.body["nav_count"].(float64)
		nav += n
	}
	sort.SliceStable(all, func(i, j int) bool {
		a, _ := all[i]["created_at"].(string)
		b, _ := all[j]["created_at"].(string)
		if a != b {
			return a > b
		}
		ai, _ := all[i]["id"].(string)
		bj, _ := all[j]["id"].(string)
		return ai > bj
	})
	body := map[string]any{"prs": all, "counts": counts, "nav_count": int(nav)}
	if len(quiet) > 0 {
		sort.Strings(quiet)
		body["rooms_quiet"] = quiet
	}
	if len(without) > 0 {
		sort.Strings(without)
		body["rooms_without"] = without
	}
	writeJSONBody(w, http.StatusOK, body)
	return true
}

// askPulls is one `GET /v1/prs` against one room. `without` is a 404: the room is up and its build has no pulls. Any
// other failure is an error and the room is quiet.
//
// BOUNDED LIKE `askRoom`, at 20 seconds, on the request and not on the client, which carries the event streams.
func (p *Proxy) askPulls(r *http.Request, room string) (body map[string]any, without bool, err error) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+hostFor(room)+r.URL.RequestURI(), nil)
	if err != nil {
		return nil, false, err
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return nil, false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return nil, true, nil
	}
	if res.StatusCode != http.StatusOK {
		return nil, false, errStatus(res.StatusCode)
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&body); err != nil {
		return nil, false, err
	}
	return body, false, nil
}
