package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The aggregate view: every room, one board.
//
// ── the mode this file exists for ────────────────────────
//
// Scoped is a byte pipe and needs none of this. Aggregate cannot be a pipe,
// because you cannot forward one request to four rooms and concatenate the
// answers. So the hub parses, merges, and hands back one payload, which is the
// thing `docs/federation-design-v2.md` ruled out and the operator overrode
// knowingly. See `docs/hub-room-requirements.md`.
//
// ── what is merged, and what is deliberately not ─────────
//
// Only the LIST endpoints, and only for reading. Everything else in aggregate
// mode is answered by asking which room, because the honest answer to "save
// this setting" across four machines is a question rather than a guess.
//
// The line the requirements draw: anything about the BOARD belongs to the hub,
// anything about a MACHINE belongs to a room. A machine-shaped write in
// aggregate mode has no room to land in, so it is refused with a sentence
// naming the fix rather than being spread across all of them.

// merged is one list endpoint the hub knows how to combine.
//
// A TABLE RATHER THAN A SWITCH, so adding one is a row. Each names the JSON
// field holding the array and whether its rows carry a card id that has to be
// tagged with the room it came from.
var merged = map[string]struct {
	field string
	// idField is the row's own id, rewritten to `room~id` so a click routes
	// back to the right room. Empty means the rows carry no id worth tagging.
	idField string
	// taskField is a row that POINTS AT a card rather than being one. A
	// permission belongs to a task, and the board builds urls from that.
	taskField string
}{
	"/v1/tasks":       {field: "tasks", idField: "id"},
	"/v1/waiting":     {field: "tasks", idField: "id"},
	"/v1/permissions": {field: "permissions", idField: "id", taskField: "task_id"},
	"/v1/offered":     {field: "items", idField: "id"},
	"/v1/shares":      {field: "shares", taskField: "task_id"},

	// THE MACHINE-SHAPED CONFIGURATION, which is everything the rooms tab
	// shows. A runner, a fixture, a source, a recogniser, an action and a rule
	// are all facts about one machine, so four rooms have four sets and the
	// hub shows all of them with the room on each row.
	//
	// THEIR IDS ARE NOT TAGGED, unlike a card's. A card id is tagged because
	// every per-card URL the board builds carries it through `/v1/tasks/<id>`,
	// which the hub knows how to untag. These ids appear in their own paths,
	// which it does not, so a tag here would be a 404 on the way back. Two
	// rooms can both have a runner called `claude` and that is not a
	// collision: they are two runners, told apart by the room on the row, and
	// a write says which room in a header.
	"/v1/harnesses":   {field: "harnesses"},
	"/v1/fixtures":    {field: "fixtures"},
	"/v1/sources":     {field: "sources"},
	"/v1/recognisers": {field: "recognisers"},
	"/v1/actions":     {field: "actions"},
	"/v1/rules":       {field: "rules"},

	// The queue of work handed to machines, and what was decided about
	// permission requests. Both are lists of things that happened on a
	// machine, so all of them together is the answer a hub wants.
	"/v1/dispatch":            {field: "dispatches"},
	"/v1/permissions/history": {field: "permissions", idField: "id", taskField: "task_id"},
}

// borrowed are reads the board needs to draw itself at all, which are answered
// by ONE room rather than refused.
//
// They are machine-shaped and merging them would be a lie: four machines have
// four editor commands and four sets of terminal themes. But none of them is
// being asked as a question about a machine. The board asks for its settings to
// decide how to render, and answering "pick a room first" left it collecting
// 409s and drawing empty panes, which is worse than one machine's answer.
//
// Reading only. Writing one still asks, because that is a question about a
// machine and the answer matters.
var borrowed = map[string]bool{
	"/v1/settings": true,
	"/v1/themes":   true,
	"/v1/rooms":    true,
	"/v1/hooks":    true,
	// Runners this machine has that are not set up yet. Merging would be a
	// list of one machine's `ollama` beside another's, which is a table of
	// things to add somewhere unspecified.
	"/v1/harnesses/discover": true,
	// WHO MAY OPEN THE BOARD, AND WHERE IT IS PUBLISHED, which
	// `docs/hub-room-requirements.md` says belong to the hub: there is one
	// board and publishing it is the hub's job. Until they move, one room's
	// answer is what the board draws, and drawing nothing was worse.
	"/v1/auth":     true,
	"/v1/overlays": true,
}

// firstRoom is a room to borrow an answer from, chosen the same way every time
// so two requests in one page load cannot disagree.
func (p *Proxy) firstRoom() string {
	rooms := p.hub.Rooms()
	names := make([]string, 0, len(rooms))
	for _, r := range rooms {
		names = append(names, r.Name)
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

// aggregate answers a list endpoint from every attached room.
//
// ONE ROOM FAILING IS NOT THE REQUEST FAILING. A room that is mid-restart, or
// whose machine went to sleep, contributes nothing and the rest of the board
// still draws. The alternative is a board that goes blank because one of four
// machines is busy, which is worse than a board that is briefly short.
func (p *Proxy) aggregate(w http.ResponseWriter, r *http.Request, spec string) bool {
	// READS ONLY, AND THE METHOD IS THE WHOLE CHECK.
	//
	// `/v1/tasks` is a merged list and it is also where a card is created.
	// Without this, `POST /v1/tasks` with two rooms attached would fan out as
	// four GETs and answer 200 with a task list, so the card would silently
	// never be made and the board would have no way to tell. A write with no
	// room to land in falls through to `needsARoom`, which asks.
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		return false
	}
	m, ok := merged[spec]
	if !ok {
		return false
	}
	rooms := p.hub.Rooms()
	if len(rooms) == 0 {
		return false
	}

	type answer struct {
		room string
		rows []any
		err  error
	}
	out := make([]answer, len(rooms))
	var wg sync.WaitGroup
	for i, room := range rooms {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			out[i].room = name
			rows, err := p.askRoom(r, name, m.field)
			out[i].rows, out[i].err = rows, err
		}(i, room.Name)
	}
	wg.Wait()

	all := []any{}
	var quiet []string
	for _, a := range out {
		if a.err != nil {
			quiet = append(quiet, a.room)
			continue
		}
		for _, row := range a.rows {
			obj, ok := row.(map[string]any)
			if !ok {
				all = append(all, row)
				continue
			}
			// THE ROOM GOES ON THE ROW, twice over.
			//
			// `room` is the field the board draws as a tag, which is what the
			// operator asked for so it can be filtered on later. The id rewrite
			// is what makes the row CLICKABLE: every per-card url the board
			// builds comes from this id, so carrying the room in it routes the
			// click without the board knowing rooms exist.
			obj["room"] = a.room
			if m.idField != "" {
				if id, ok := obj[m.idField].(string); ok {
					obj[m.idField] = tagFor(a.room, id)
				}
			}
			if m.taskField != "" {
				if id, ok := obj[m.taskField].(string); ok {
					obj[m.taskField] = tagFor(a.room, id)
				}
			}
			all = append(all, obj)
		}
	}

	body := map[string]any{m.field: all}
	if len(quiet) > 0 {
		// SAID, NOT SWALLOWED. A short board with no explanation is a board
		// somebody debugs. The board can draw this or ignore it, and either way
		// the fact is in the payload rather than only in a log.
		sort.Strings(quiet)
		body["rooms_quiet"] = quiet
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
	return true
}

// history is every machine's finished work, in one list, newest first.
//
// ── why this is not a row in `merged` ────────────────────
//
// History is PAGED, and paging is where a naive fan-out quietly goes wrong.
// Asking four rooms for fifty rows each and concatenating gives two hundred,
// and asking again at offset fifty then skips whatever the first answer had
// already shown. The board would lose rows and nothing would look broken.
//
// So the hub asks every room for everything up to the end of the page being
// drawn, merges, sorts, and cuts the page out of the merged list. Page three
// costs re-reading pages one and two from each room, which is a few hundred
// rows of already-indexed reads and is the price of the pages being correct.
//
// ── and this one IS sorted ───────────────────────────────
//
// `docs/hub-room-requirements.md` says cross-room order is not a guarantee,
// and that stands for the live lists: they carry no shared clock and sorting
// them would claim one. History is different. Every row has a creation time,
// the whole point of the list is chronology, and "newest first" is what it
// means. So it is sorted here, on the field the rooms themselves order by.
func (p *Proxy) history(w http.ResponseWriter, r *http.Request) {
	rooms := p.hub.Rooms()
	if len(rooms) == 0 {
		return
	}
	q := r.URL.Query()
	limit, _ := strconv.Atoi(q.Get("limit"))
	if limit <= 0 {
		limit = 50
	}
	offset, _ := strconv.Atoi(q.Get("offset"))
	if offset < 0 {
		offset = 0
	}

	// Everything up to the end of the page being drawn, from each room.
	ask := *r.URL
	vals := ask.Query()
	vals.Set("limit", strconv.Itoa(offset+limit))
	vals.Set("offset", "0")
	ask.RawQuery = vals.Encode()
	scan := r.Clone(r.Context())
	scan.URL = &ask

	type answer struct {
		room  string
		rows  []any
		total float64
		err   error
	}
	out := make([]answer, len(rooms))
	var wg sync.WaitGroup
	for i, room := range rooms {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			out[i].room = name
			body := p.askRoomBody(scan, name)
			if body == nil {
				out[i].err = errStatus(http.StatusBadGateway)
				return
			}
			out[i].rows, _ = body["tasks"].([]any)
			out[i].total, _ = body["total"].(float64)
		}(i, room.Name)
	}
	wg.Wait()

	all := []map[string]any{}
	var quiet []string
	total := 0
	for _, a := range out {
		if a.err != nil {
			quiet = append(quiet, a.room)
			continue
		}
		total += int(a.total)
		for _, row := range a.rows {
			obj, ok := row.(map[string]any)
			if !ok {
				continue
			}
			obj["room"] = a.room
			if id, ok := obj["id"].(string); ok {
				obj["id"] = tagFor(a.room, id)
			}
			all = append(all, obj)
		}
	}
	// Newest first, the same order every room used. Ties break on the tagged
	// id so the sort is stable and a page boundary cannot show one row twice.
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

	page := []map[string]any{}
	if offset < len(all) {
		end := offset + limit
		if end > len(all) {
			end = len(all)
		}
		page = all[offset:end]
	}
	body := map[string]any{"tasks": page, "total": total, "offset": offset}
	if len(quiet) > 0 {
		sort.Strings(quiet)
		body["rooms_quiet"] = quiet
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(body)
}

// health is the board asking whether atrium is up, in aggregate.
//
// IT CANNOT BE PROXIED AND IT CANNOT BE REFUSED. The board polls it to decide
// whether to draw at all, so answering "pick a room" would leave a hub with
// four rooms looking exactly like a hub with none. And it cannot be forwarded
// to one room either, because the answer is about all of them.
//
// So it is merged, and the merge is pessimistic on purpose: one halted room
// means the board says halted. A halt is a machine that has stopped recording
// what its agents do, and a board that hides that behind three healthy rooms
// is a board that lies at the one moment it matters.
func (p *Proxy) health(w http.ResponseWriter, r *http.Request) {
	rooms := p.hub.Rooms()
	out := map[string]any{
		// THE HUB'S OWN BOARD HASH, for the same reason `rewriteHealth` swaps
		// it on the scoped path: the browser is running the hub's copy of the
		// board, so a room's hash here would reload every tab forever.
		"ok": true, "build": p.boardID, "rooms": len(rooms),
	}
	type answer struct {
		room string
		body map[string]any
	}
	got := make([]answer, len(rooms))
	var wg sync.WaitGroup
	for i, room := range rooms {
		wg.Add(1)
		go func(i int, name string) {
			defer wg.Done()
			got[i].room = name
			got[i].body = p.askRoomBody(r, name)
		}(i, room.Name)
	}
	wg.Wait()

	var quiet, halted []string
	settling := false
	for _, a := range got {
		if a.body == nil {
			quiet = append(quiet, a.room)
			continue
		}
		if yes, _ := a.body["halted"].(bool); yes {
			halted = append(halted, a.room)
			if out["cause"] == nil {
				out["cause"] = fmt.Sprintf("%s: %v", a.room, a.body["cause"])
			}
		}
		if yes, _ := a.body["settling"].(bool); yes {
			settling = true
		}
	}
	sort.Strings(quiet)
	sort.Strings(halted)
	out["halted"] = len(halted) > 0
	out["halted_rooms"] = halted
	out["settling"] = settling
	if len(quiet) > 0 {
		out["rooms_quiet"] = quiet
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(out)
}

// askRoomBody is one GET against one room, decoded whole. Nil for a room that
// did not answer, which every caller treats as quiet rather than as an error.
func (p *Proxy) askRoomBody(r *http.Request, room string) map[string]any {
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+hostFor(room)+r.URL.RequestURI(), nil)
	if err != nil {
		return nil
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil
	}
	var body map[string]any
	if err := json.NewDecoder(io.LimitReader(res.Body, 4<<20)).Decode(&body); err != nil {
		return nil
	}
	return body
}

// askRoom runs one request against one room and pulls out the array.
func (p *Proxy) askRoom(r *http.Request, room, field string) ([]any, error) {
	// A FAN-OUT WAITS FOR THE SLOWEST ROOM, so the bound lives here, on the
	// request, and not on the client. The client is shared with the event
	// streams, which are meant to stay open for hours.
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		"http://"+hostFor(room)+r.URL.RequestURI(), nil)
	if err != nil {
		return nil, err
	}
	res, err := p.roomClient(room).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, errStatus(res.StatusCode)
	}
	var body map[string]any
	// Bounded. A room's task list is large and a room that answers with
	// something enormous must not be able to exhaust the hub.
	if err := json.NewDecoder(io.LimitReader(res.Body, 32<<20)).Decode(&body); err != nil {
		return nil, err
	}
	rows, _ := body[field].([]any)
	return rows, nil
}

type errStatus int

func (e errStatus) Error() string { return "the room answered " + http.StatusText(int(e)) }

// roomClient is a client pinned to one room.
//
// One per room, kept, because `http.Transport` is where the keep-alive lives
// and making a new one per request would dial a fresh connection every time
// and discard the pool this whole design is built around.
//
// NO CLIENT TIMEOUT, and that is deliberate rather than forgotten. This client
// carries the event stream as well as the list fan-out, and an event stream
// sends its first byte when something happens, which may be an hour from now.
// The fan-out bounds itself on its own request context instead.
func (p *Proxy) roomClient(room string) *http.Client {
	p.mu.Lock()
	defer p.mu.Unlock()
	if c, ok := p.clients[room]; ok {
		return c
	}
	c := &http.Client{
		Transport: &http.Transport{
			DialContext:         p.hub.dialer(room),
			MaxIdleConns:        16,
			MaxIdleConnsPerHost: 16,
			IdleConnTimeout:     90 * time.Second,
			// Zero, meaning no limit, for the reason above.
			ResponseHeaderTimeout: 0,
		},
	}
	if p.clients == nil {
		p.clients = map[string]*http.Client{}
	}
	p.clients[room] = c
	return c
}

// needsARoom is the refusal for a write with no room to land in.
//
// The alternative is spreading a machine-shaped setting across every machine,
// which is a guess wearing the clothes of a feature.
func needsARoom(w http.ResponseWriter, rooms []Attached) {
	names := make([]string, 0, len(rooms))
	for _, r := range rooms {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusConflict)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": "that belongs to one machine, and you are looking at all of them. " +
			"pick a room first: " + strings.Join(names, ", "),
		"rooms": names,
	})
}
