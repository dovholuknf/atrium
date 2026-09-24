package link

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/inputlag"
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
	// Whether a zrok account is enabled on that machine. Read while the
	// overlays pane draws, so refusing it left the pane unable to say whether
	// sharing was even possible.
	"/v1/overlays/zrok/account": true,
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
	// WHAT THE ROOMS THAT ARE NOT ANSWERING LAST SAID, worked out first,
	// because it decides whether there is an answer at all.
	//
	// With no room attached this used to fall through to "no room is attached",
	// which is the right thing to say about a hub that has never had one and
	// the wrong thing to say to somebody whose two laptops are shut. They still
	// have work on them, the hub remembers what it was, and a board that says
	// "nothing to show" is a board that appears to have lost it.
	old := p.remembered(spec, rooms)
	if len(rooms) == 0 && len(old) == 0 {
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

	// AND THE ROOMS THAT ARE NOT ANSWERING, for the one list where their work
	// still exists.
	all = append(all, old...)

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

// remembered is what the rooms that are NOT answering last said they held.
//
// ── why only the cards ──────────────────────────────────
//
// Cards are work. A machine somebody shut is still holding it, and a board that
// simply dropped those rows would say the work does not exist. Everything else
// in `merged` is configuration: an offline room's runners and fixtures describe
// how that machine is set up, which is worth nothing while it cannot be reached
// and is not work anybody is looking for.
//
// ── and only for a room that has connected before ───────
//
// A room that never has cannot have cards, so there is nothing to draw. It
// exists in the rooms tab, which is the inventory, and that is the only place
// it belongs.
//
// Every row is marked `offline`, which is what the board reads to put them in
// their own collapsed group and to refuse to open any of them. Nothing here is
// counted anywhere: every badge on the board is live only, because a number you
// cannot act on is a number that makes you look.
func (p *Proxy) remembered(spec string, live []Attached) []any {
	if spec != "/v1/tasks" {
		return nil
	}
	stock := p.inventory()
	if stock == nil {
		return nil
	}
	// ONE QUESTION FOR THE WHOLE LIST. This runs on every board request, so it
	// asks which rooms have anything remembered rather than reading every room
	// and then counting each one's cards.
	names, err := stock.Holding()
	if err != nil {
		log.Printf("[hub] could not read what the offline rooms were holding: %v", err)
		return nil
	}
	here := map[string]bool{}
	for _, a := range live {
		here[keyOf(a.Name)] = true
	}

	var out []any
	for _, name := range names {
		// A ROOM THAT IS HERE ANSWERS FOR ITSELF. The cache is written while a
		// room is connected and read only when it is not, so a live room's
		// remembered cards are never drawn beside its real ones.
		if here[keyOf(name)] {
			continue
		}
		cards, err := stock.Remembered(name)
		if err != nil {
			log.Printf("[hub] could not read %q's last known cards: %v", name, err)
			continue
		}
		for _, c := range cards {
			var obj map[string]any
			if err := json.Unmarshal(c.Payload, &obj); err != nil || obj == nil {
				continue
			}
			obj["room"] = name
			// THE SAME TAGGING A LIVE CARD GETS, so the board builds the same
			// urls from it. Those urls will be refused, by name, which is
			// better than a card whose buttons quietly do nothing.
			obj["id"] = tagFor(name, c.ID)
			// WHAT A ROOM WOULD HAVE COMPUTED ON THE WAY OUT.
			//
			// The cache holds the stored row, which is the rule: never what the
			// room declines to persist. `display_title` is not one of those. It
			// is not live state, it is the observed-versus-overrides rule
			// applied to two fields that ARE stored, and a room derives it fresh
			// on every request for exactly that reason.
			//
			// So it is derived here too, rather than cached. Without it an
			// offline card draws with no title at all, which is the one thing
			// somebody needs to recognise the work they are looking at.
			derive(obj)
			// WHAT THE BOARD DRAWS DIFFERENTLY. One flag rather than a shape of
			// its own, because it is the same card: it is the room that is
			// missing, not the work.
			obj["offline"] = true
			out = append(out, obj)
		}
	}
	return out
}

// derive fills the fields a room computes on the way out, for a card that came
// from the cache instead.
//
// ONLY THE ONES THAT ARE A FUNCTION OF STORED FIELDS. An override wins over the
// observed value, which is the rule in `docs/architecture-v2.md` and the whole
// of what these two are. Nothing here reaches for anything the room declined to
// write down: there is no activity, no telemetry and no idle time, because
// those are true only while a room is running and this room is not.
func derive(obj map[string]any) {
	over, _ := obj["overrides"].(map[string]any)
	pick := func(field, observed string) string {
		if over != nil {
			if v, ok := over[field].(string); ok && v != "" {
				return v
			}
		}
		s, _ := obj[observed].(string)
		return s
	}
	if _, ok := obj["display_title"]; !ok {
		obj["display_title"] = pick("title", "title")
	}
	if _, ok := obj["display_repo"]; !ok {
		obj["display_repo"] = pick("repo", "repo")
	}
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

// hubSettings answers `/v1/settings` in the ALL scope so the board skin is the
// HUB's own, not one borrowed from whichever room sorts first.
//
// ── the seam, and why it is here ─────────────────────────
//
// `/v1/settings` is a borrowed read (see `borrowed`): the board asks for it to
// draw itself, and the hub hands back one room's answer because refusing left
// the board collecting 409s. That borrow is right for editor commands and
// terminal themes, which the board only reads and which genuinely belong to a
// machine. It is wrong for the skin, which is board-wide and the hub's to own:
// a second room attaching would swap the ALL view to that room's skin, and a
// skin saved from ALL had no room to land in and got the `needsARoom` 409.
//
// So the skin, and only the skin, is lifted off the borrow. A GET still borrows
// the whole settings payload and then overwrites `board_skin` with the hub's. A
// skin-only POST writes the hub's skin. Every other field, and every write that
// names another setting, is left exactly as it was.
//
// Returns true when it answered, false to fall through to the borrow.
func (p *Proxy) hubSettings(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Path != "/v1/settings" {
		return false
	}
	stock := p.inventory()
	if stock == nil {
		return false // a hub with no durable store has no skin of its own
	}
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		body, ok := p.hubSettingsBody(r, stock)
		if !ok {
			return false
		}
		writeJSONBody(w, http.StatusOK, body)
		return true
	case http.MethodPost, http.MethodPut:
		// Read once and route on what the body names. The board's board-wide
		// switch posts `global_auto`, the skin picker posts `board_skin`, and
		// they are never the same request. Anything else falls through to the
		// borrow, which asks which room it is for.
		payload, err := io.ReadAll(io.LimitReader(r.Body, 1<<16))
		if err != nil {
			return false
		}
		var keys map[string]json.RawMessage
		if err := json.Unmarshal(payload, &keys); err != nil {
			return false
		}
		if _, ok := keys["global_auto"]; ok {
			return p.saveBoardAuto(w, payload, stock)
		}
		// The public-share login, set on the settings screen. Routed on any of
		// its keys being present, ahead of the skin save, because it is a
		// board-owned setting the hub holds and it is never a machine-shaped
		// write with a room to land in. See saveShareAuth and the design.
		for _, k := range []string{"share_auth", "share_user", "share_pass", "share_oidc"} {
			if _, ok := keys[k]; ok {
				return p.saveShareAuth(w, r, payload, stock)
			}
		}
		return p.saveHubSkin(w, r, payload, stock)
	}
	return false
}

// maxBoardAutoMinutes bounds how long the board-wide switch can be left on, the
// same day the room's own switch uses and for the same reason: "for the next
// hour" is the shape this is for, and a deadline in three weeks is a switch left
// on with extra steps. See internal/api maxAutoMinutes.
const maxBoardAutoMinutes = 24 * 60

// applyBoardAuto overwrites the switch fields in a settings payload with the
// HUB's flag, so the ALL view shows one board-wide answer rather than whichever
// room it borrowed the payload from.
func (p *Proxy) applyBoardAuto(body map[string]any, stock Inventory) {
	on, until, err := stock.BoardAuto()
	if err != nil {
		on, until = false, nil
	}
	body["global_auto"] = on
	// A stale deadline borrowed from a room must not survive: the hub's flag is
	// the answer now, and a leftover `global_auto_until` would read as a switch
	// expiring at a time the hub never set.
	delete(body, "global_auto_until")
	delete(body, "global_auto_seconds")
	if until != nil {
		body["global_auto_until"] = until.Format(time.RFC3339)
		// Seconds left, so the board does not have to agree with the hub about
		// what time it is, the same rule the room's own view follows.
		if left := time.Until(*until); left > 0 {
			body["global_auto_seconds"] = int64(left.Seconds())
		}
	}
}

// boardAutoView is the switch as the board reads it back after a save, built
// from the hub flag alone so an old tab and a fresh one cannot disagree.
func (p *Proxy) boardAutoView(stock Inventory) map[string]any {
	out := map[string]any{}
	p.applyBoardAuto(out, stock)
	return out
}

// saveBoardAuto lands the board-wide switch on the HUB, which is how the toggle
// in the ALL view lands somewhere instead of being refused for want of a room.
//
// It does NOT touch any room's own `global_auto`: a room-scoped view still turns
// that room loose on its own. This is the wider switch, enforced hub-side on the
// permission relay. See autoapprove.go.
func (p *Proxy) saveBoardAuto(w http.ResponseWriter, payload []byte, stock Inventory) bool {
	var body struct {
		GlobalAuto *bool `json:"global_auto"`
		Minutes    int   `json:"global_auto_minutes"`
	}
	if err := json.Unmarshal(payload, &body); err != nil || body.GlobalAuto == nil {
		return false
	}
	if body.Minutes < 0 || body.Minutes > maxBoardAutoMinutes {
		writeErrBody(w, http.StatusBadRequest, fmt.Sprintf(
			"board-wide auto can be left on for up to %d minutes, or with no deadline at all",
			maxBoardAutoMinutes))
		return true
	}
	var until *time.Time
	if *body.GlobalAuto && body.Minutes > 0 {
		t := time.Now().UTC().Add(time.Duration(body.Minutes) * time.Minute)
		until = &t
	}
	if err := stock.SetBoardAuto(*body.GlobalAuto, until); err != nil {
		writeErrBody(w, http.StatusInternalServerError, err.Error())
		return true
	}
	// Turning it on empties the queue at once rather than on the approver's next
	// tick, which is what a person expects from a button they just pressed. See
	// `docs/auto-mode.md`, "turning it on empties the queue".
	if *body.GlobalAuto && p.approver != nil {
		p.approver.nudge()
	}
	writeJSONBody(w, http.StatusOK, p.boardAutoView(stock))
	return true
}

// hubSettingsBody borrows one room's whole settings answer and swaps in the
// hub's skin. False when there is no room to borrow from, which leaves the
// caller to fall through: a hub with no room attached has no borrowed payload to
// build on, and the skin alone is not a settings page.
func (p *Proxy) hubSettingsBody(r *http.Request, stock Inventory) (map[string]any, bool) {
	if p.firstRoom() == "" {
		return nil, false
	}
	// askRoomBody builds its own GET, so a POST request handed here still borrows
	// the settings correctly. Every field but the skin is left as the room wrote
	// it.
	body := p.askRoomBody(r, p.firstRoom())
	if body == nil {
		return nil, false
	}
	body["board_skin"] = p.hubSkinClamped(stock, body)
	// The board-wide switch is the hub's answer too, not the borrowed room's, so
	// the ALL view shows one board-wide state rather than whichever room sorted
	// first. See applyBoardAuto and autoapprove.go.
	p.applyBoardAuto(body, stock)
	// And the public-share login, which is the hub's the same way. See
	// applyShareAuth.
	p.applyShareAuth(body, stock)
	// And the input-lag switch, which in the ALL view is the hub's own answer
	// rather than the borrowed room's. See inputlagsetting.go.
	body["input_lag_log"] = inputlag.On()
	body["input_lag_pinned"] = inputlag.Pinned()
	return body, true
}

// applyShareAuth writes the public-share login into a settings payload: the
// scheme, the updb username and the oidc provider as stored, plus whether a
// password is set. THE PASSWORD ITSELF IS NEVER SENT. It guards the very board
// this payload travels over, so the board is told only that one exists, which is
// all a settings screen needs to draw "set" versus "not set".
func (p *Proxy) applyShareAuth(body map[string]any, stock Inventory) {
	a, err := stock.ShareAuth()
	if err != nil {
		a = ShareAuth{}
	}
	body["share_auth"] = a.Scheme
	body["share_user"] = a.User
	body["share_oidc"] = a.OIDCProvider
	body["share_pass_set"] = a.Pass != ""
}

// shareAuthView is the login as the board reads it back after a save, built from
// the hub's stored value alone so an old tab and a fresh one cannot disagree.
func (p *Proxy) shareAuthView(stock Inventory) map[string]any {
	out := map[string]any{}
	p.applyShareAuth(out, stock)
	return out
}

// saveShareAuth lands the public-share login on the hub, which is how the
// settings screen's share user/pass field lands somewhere instead of being
// refused for want of a room.
//
// ── validation is where the design's rule lives ──────────
//
// The scheme is "updb", "oidc" or "" (none). updb needs a username and, unless
// one is already stored, a password: a public share with an empty credential is
// the whole internet with an extra step, which the design refuses at
// configuration time rather than at share time. oidc needs a provider. None is
// allowed to be saved, because turning the login off and then not opening a
// public share is a legitimate state; the refusal that matters is at the point a
// PUBLIC share would actually be created (see cmd/atrium2/hubshare.go), which is
// the only place that knows a public share is being asked for.
//
// The password is written only when the body carries a non-empty one, so a save
// that leaves the box blank keeps the stored password. An explicit empty string
// in the body clears it, which is the one way to remove a password.
func (p *Proxy) saveShareAuth(w http.ResponseWriter, r *http.Request, payload []byte, stock Inventory) bool {
	// ONLY THE SHARE-LOGIN KEYS, AND NOTHING ELSE. A body that also names a
	// machine-shaped setting still has no room to land in, so it falls through to
	// `needsARoom` rather than being applied by halves. The share login alone is
	// the escape hatch the hub owns, the same rule saveHubSkin follows.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return false
	}
	allowed := map[string]bool{"share_auth": true, "share_user": true, "share_pass": true, "share_oidc": true}
	for k := range raw {
		if !allowed[k] {
			return false
		}
	}
	var body struct {
		Scheme   *string `json:"share_auth"`
		User     *string `json:"share_user"`
		Pass     *string `json:"share_pass"`
		Provider *string `json:"share_oidc"`
	}
	if err := json.Unmarshal(payload, &body); err != nil {
		return false
	}
	// Start from what is stored and overlay only what the body names, so a save
	// that mentions one field does not blank the others.
	cur, err := stock.ShareAuth()
	if err != nil {
		writeErrBody(w, http.StatusInternalServerError, err.Error())
		return true
	}
	if body.Scheme != nil {
		cur.Scheme = strings.ToLower(strings.TrimSpace(*body.Scheme))
	}
	if body.User != nil {
		cur.User = strings.TrimSpace(*body.User)
	}
	if body.Provider != nil {
		cur.OIDCProvider = strings.TrimSpace(*body.Provider)
	}
	// THE PASSWORD BOX: a blank box means "keep the stored password", not "clear
	// it". A password is not shown back to the board (only whether one is set),
	// so a settings screen reopened after a save shows an empty box even though a
	// password is stored, and treating that empty box as a clear would wipe the
	// credential every time somebody saved an unrelated change. So only a
	// non-empty value changes the stored password. The store's SetShareAuth
	// follows the same rule on the way down.
	if body.Pass != nil && strings.TrimSpace(*body.Pass) != "" {
		cur.Pass = *body.Pass
	}

	switch cur.Scheme {
	case "", "updb", "oidc":
	default:
		writeErrBody(w, http.StatusBadRequest, fmt.Sprintf(
			"no share login called %q. one of: updb, oidc, or empty for none", cur.Scheme))
		return true
	}
	if cur.Scheme == "updb" {
		if cur.User == "" {
			writeErrBody(w, http.StatusBadRequest,
				"a username and password guard a public share with updb. set the username")
			return true
		}
		// A password must exist in the end, whether it was already stored or is
		// being set now. Switching only the username, with a password already
		// stored, is fine; setting updb with nothing stored and no password given
		// is refused, so a public share is never left with an empty credential.
		if cur.Pass == "" {
			writeErrBody(w, http.StatusBadRequest,
				"a public share with updb needs a password. set one")
			return true
		}
	}
	if cur.Scheme == "oidc" && cur.OIDCProvider == "" {
		writeErrBody(w, http.StatusBadRequest,
			"an oidc share needs a provider. name the one your zrok account is configured with")
		return true
	}

	if err := stock.SetShareAuth(cur); err != nil {
		writeErrBody(w, http.StatusInternalServerError, err.Error())
		return true
	}
	writeJSONBody(w, http.StatusOK, p.shareAuthView(stock))
	return true
}

// hubSkinClamped is the hub's stored skin, kept inside the list the board
// actually ships. An unset or unknown value comes back as the default, which the
// board reports as the FIRST entry of `board_skins`: that list is ordered
// default-first, so a fresh hub, or one left on a name a later build dropped,
// looks like a fresh board rather than like a setting that was ignored.
func (p *Proxy) hubSkinClamped(stock Inventory, borrowed map[string]any) string {
	skin, err := stock.HubSkin()
	if err != nil {
		skin = ""
	}
	skin = strings.TrimSpace(skin)
	names := skinNames(borrowed)
	if skin == "" || !contains(names, skin) {
		if len(names) > 0 {
			return names[0]
		}
	}
	return skin
}

// saveHubSkin takes a skin-only save in the ALL scope onto the hub.
//
// ONLY THE SKIN, AND NOTHING ELSE. A body that also names a machine-shaped
// setting still has no room to land in, so it is left for `needsARoom` to ask
// which room. A save that is purely the skin is the one the hub owns.
func (p *Proxy) saveHubSkin(w http.ResponseWriter, r *http.Request, payload []byte, stock Inventory) bool {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(payload, &raw); err != nil {
		return false // not a shape we handle; let the borrow refuse it
	}
	if len(raw) != 1 {
		return false
	}
	skinRaw, ok := raw["board_skin"]
	if !ok {
		return false
	}
	var name string
	if err := json.Unmarshal(skinRaw, &name); err != nil {
		writeErrBody(w, http.StatusBadRequest, "board_skin must be a string")
		return true
	}
	name = strings.TrimSpace(name)
	// Validated against the skins the board ships, the same refusal a room makes
	// on the way in, so an unknown name is told at the boundary rather than saved
	// and silently worn as the default. The list is borrowed from a room; with
	// none attached there is nothing to check against, so the name is stored as
	// sent and the read-side clamp is the safety net.
	if name != "" {
		if body := p.askRoomBody(r, p.firstRoom()); body != nil {
			names := skinNames(body)
			if len(names) > 0 && !contains(names, name) {
				writeErrBody(w, http.StatusBadRequest,
					fmt.Sprintf("no skin called %q. the ones there are: %s",
						name, strings.Join(names, ", ")))
				return true
			}
		}
	}
	if err := stock.SetHubSkin(name); err != nil {
		writeErrBody(w, http.StatusInternalServerError, err.Error())
		return true
	}
	// Answer with the settings the board would now read, so its cached prefs pick
	// up the new skin and still carry the skin list. With no room to borrow from,
	// the skin alone is the honest answer.
	if body, ok := p.hubSettingsBody(r, stock); ok {
		writeJSONBody(w, http.StatusOK, body)
		return true
	}
	writeJSONBody(w, http.StatusOK, map[string]any{"board_skin": name})
	return true
}

// skinNames pulls the list of shipped skins out of a borrowed settings payload.
func skinNames(body map[string]any) []string {
	raw, _ := body["board_skins"].([]any)
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func writeJSONBody(w http.ResponseWriter, code int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

func writeErrBody(w http.ResponseWriter, code int, msg string) {
	writeJSONBody(w, code, map[string]any{"error": msg})
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
