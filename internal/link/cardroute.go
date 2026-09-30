package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// A REQUEST THAT NAMES A CARD GOES TO THE ROOM THAT HOLDS IT. Backlog-2 item 63.
//
// Two ways this went wrong with three rooms attached, both from the same board
// header. The board's header on a write is `writeRoom`, a board-global left by
// the last editor that was open, whenever the url is not `/v1/tasks/<id>/<verb>`.
// So a start (`POST /v1/launch`, card in the BODY) and a drag into a group
// (`PATCH /v1/tasks/<id>`, a plain id in the PATH) both went to whichever room
// that editor was for, and came back "sql: no rows in result set".
//
// The card decides, never the header. A header can only be wrong about a card: a
// card id is globally unique, so exactly one room holds it.
//
//   - A `room~id` tag, in the path or the body, names the room outright.
//   - A plain id is looked up across the attached rooms, when there are two or
//     more. See `roomHolding`.
//   - A card no attached room holds is a 404 naming the card and the rooms asked,
//     never a guess and never the room's own "no rows".
//
// ONLY THE LAUNCH READS ITS BODY. Every other call that names a card (reopen,
// restart, unshelve, message, exit, a group or tag change) names it in the path.
// `/v1/intake` and `/v1/dispatch` make new work and name no existing card.

// launchBodyLimit is the room's own limit on a launch body. See `launch` in
// internal/api/api.go. A body past it is forwarded untouched, since the room
// refuses it anyway.
const launchBodyLimit = 1 << 20

// notCards are the `/v1/tasks/<segment>` routes whose segment is not a card id.
var notCards = map[string]bool{"prune": true, "pin-order": true, "archive-workers": true}

// cardRoomKey carries the room the named card was placed on, ahead of every
// other way `roomFor` has of choosing one.
type cardRoomKey struct{}

// placeCard routes a request that names a card to the room holding it.
//
// It returns the request to carry on with, or false once it has answered.
//
// A NAME IS RESOLVED HERE, BEFORE ANY ROOM SEES IT, in this order. See
// docs/rnd/handle-addressed-http-design.md section 4.
//
//  1. `room~id`, a tagged id, names the room outright.
//  2. `name@room` is looked for on that room only.
//  3. Something shaped like an id goes the way every board request goes: with
//     two or more rooms, `roomHolding` finds the owner.
//  4. Anything else is a wire name or an alias, looked for on every attached
//     room. One live match wins, two is a 409 naming both, none is a 404 with
//     what would have worked.
//
// The request then goes on with the bare id in its path or its launch body, so
// a room on any build is reached by name.
func (p *Proxy) placeCard(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	id := cardNamedIn(r)
	if id == "" {
		return r, true
	}
	room, bare := splitTag(id)
	if room != "" {
		// A tag came in, so a tag goes back out on the answer, the launch's as
		// well as a tagged path's. See `retagCard`.
		r = r.WithContext(context.WithValue(r.Context(), taggedKey{}, room))
		return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, room)), true
	}
	rooms := p.hub.Rooms()
	if name, target, err := SplitAddress(id); err == nil && target != "" {
		return p.placeByName(w, r, id, name, target, rooms)
	}
	if len(rooms) < 2 {
		// ONE ROOM OR NONE IS NOTHING TO CHOOSE BETWEEN for an id, and the dial
		// goes to the only room. A segment that is not shaped like one may be a
		// name the room cannot read, so it is looked for first, and a miss still
		// goes to the room, which answers for itself.
		if len(rooms) == 0 || looksLikeCardID(bare) {
			return r, true
		}
		return p.placeByName(w, r, id, id, "", rooms)
	}
	// AN ID GOES THE WAY EVERY BOARD REQUEST GOES. Anything not shaped like one
	// is a name first, and only a name no list carries is asked for as an id
	// (placeByName), so a name does not pay for a fan-out that can only miss.
	if looksLikeCardID(bare) {
		if owner, ok := p.roomHolding(r, bare, rooms); ok {
			return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, owner)), true
		}
		cardUnplaced(w, bare, rooms, p.rememberedOn(bare))
		return r, false
	}
	return p.placeByName(w, r, id, id, "", rooms)
}

// looksLikeCardID is the shape of an id a room mints, a UUID. A handle is a
// name somebody chose and never has this shape. It spares the id path a name
// lookup that could only miss.
func looksLikeCardID(s string) bool {
	if len(s) != 36 {
		return false
	}
	for i, c := range s {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F') {
				return false
			}
		}
	}
	return true
}

// placeByName resolves a name on `target`, or on every attached room when
// target is empty, and carries the request on to the card it found.
func (p *Proxy) placeByName(w http.ResponseWriter, r *http.Request, seg, name, target string, rooms []Attached) (
	*http.Request, bool) {
	if target != "" {
		found := ""
		for _, a := range rooms {
			if equalFold(a.Name, target) {
				found = a.Name
			}
		}
		if found == "" {
			cardAnswer(w, http.StatusNotFound, map[string]any{
				"error": fmt.Sprintf("no room called %q is attached", target)})
			return r, false
		}
		rooms = []Attached{{Name: found}}
	}
	lists := map[string][]ctlCard{}
	var quiet []string
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, a := range rooms {
		wg.Add(1)
		go func(room string) {
			defer wg.Done()
			var body struct {
				Tasks []ctlCard `json:"tasks"`
			}
			ok := p.roomGet(r.Context(), room, "/v1/tasks", &body)
			mu.Lock()
			defer mu.Unlock()
			if ok {
				lists[room] = body.Tasks
			} else {
				quiet = append(quiet, room)
			}
		}(a.Name)
	}
	wg.Wait()
	sort.Strings(quiet)
	hit, err := resolveAcross(lists, quiet, name)
	var none *errNoCard
	var amb *errAmbiguous
	// A WRITE NEVER GUESSES PAST A ROOM THAT DID NOT ANSWER. The card somebody
	// meant may be on it, and one match elsewhere would otherwise take the exit,
	// the message or the kill. A read can say what it found, a write cannot be
	// taken back. The caller names the room.
	write := r.Method != http.MethodGet && r.Method != http.MethodHead
	quietWrite := func(found []string) (*http.Request, bool) {
		for _, q := range quiet {
			found = append(found, name+"@"+q+" (not answering)")
		}
		cardAnswer(w, http.StatusConflict, map[string]any{
			"error": fmt.Sprintf("%q may be on %s, which is not answering. name the room",
				name, strings.Join(quiet, ", ")),
			"candidates": found,
		})
		return r, false
	}
	switch {
	case errors.As(err, &amb):
		cardAnswer(w, http.StatusConflict, map[string]any{"error": err.Error(), "candidates": amb.candidates})
		return r, false
	case errors.As(err, &none):
		// ONE ROOM ANSWERS FOR ITSELF. A segment no list carries goes on as it
		// always did, and the room says what it says.
		if target == "" && len(rooms) == 1 {
			return r, true
		}
		// An id of some other shape, asked for the old way.
		if target == "" {
			if owner, ok := p.roomHolding(r, seg, rooms); ok {
				return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, owner)), true
			}
		}
		if write && target == "" && len(quiet) > 0 {
			return quietWrite(nil)
		}
		cardAnswer(w, http.StatusNotFound, map[string]any{"error": err.Error(), "would_work": none.wouldWork})
		return r, false
	case err != nil:
		cardAnswer(w, http.StatusNotFound, map[string]any{"error": err.Error()})
		return r, false
	}
	if write && target == "" && len(quiet) > 0 {
		return quietWrite([]string{hit.spelled()})
	}
	bareID := hit.Card.ID
	if !rewriteCard(r, seg, bareID) {
		cardAnswer(w, http.StatusBadRequest, map[string]any{"error": "could not name that card in the request"})
		return r, false
	}
	// WHAT IT REACHED, so a script can check before it acts again. The id as the
	// caller's scope names it, and the handle with its room.
	named := bareID
	if len(p.hub.Rooms()) > 1 {
		named = tagFor(hit.Room, bareID)
	}
	w.Header().Set("X-Atrium-Card", named)
	w.Header().Set("X-Atrium-Handle", hit.Card.Wire+"@"+hit.Room)
	if target != "" {
		// Named with its room, so the answer names its cards the same way.
		r = r.WithContext(context.WithValue(r.Context(), taggedKey{}, hit.Room))
	}
	p.rememberCardRoom(bareID, hit.Room)
	return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, hit.Room)), true
}

// rewriteCard puts the bare id where the name was: the path segment, or a
// launch body's task_id.
func rewriteCard(r *http.Request, seg, id string) bool {
	if cardIDIn(r.URL.Path) == seg {
		r.URL.Path = strings.Replace(r.URL.Path, "/v1/tasks/"+seg, "/v1/tasks/"+id, 1)
		r.URL.RawPath = ""
		return true
	}
	if r.Method != http.MethodPost || r.URL.Path != "/v1/launch" || r.Body == nil {
		return false
	}
	raw, err := io.ReadAll(r.Body)
	_ = r.Body.Close()
	if err != nil {
		return false
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(raw, &fields) != nil {
		return false
	}
	fields["task_id"], _ = json.Marshal(id)
	out, err := json.Marshal(fields)
	if err != nil {
		return false
	}
	r.Body = io.NopCloser(bytes.NewReader(out))
	r.ContentLength = int64(len(out))
	return true
}

// cardAnswer is the hub answering a card request itself, in JSON.
func cardAnswer(w http.ResponseWriter, code int, body map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(body)
}

// cardNamedIn is the card a request names, tagged or plain, or nothing.
func cardNamedIn(r *http.Request) string {
	if id := cardIDIn(r.URL.Path); id != "" && !notCards[id] {
		return id
	}
	return launchCardIn(r)
}

// launchCardIn reads the `task_id` off a launch and leaves the body readable
// again for the room. A tagged `room~id` is rewritten bare on the way, because
// the room keys its cards by the id it minted.
func launchCardIn(r *http.Request) string {
	if r.Method != http.MethodPost || r.URL.Path != "/v1/launch" || r.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, launchBodyLimit+1))
	if err != nil || len(raw) > launchBodyLimit {
		// Put back what was read ahead of what was not, and route as before.
		r.Body = struct {
			io.Reader
			io.Closer
		}{io.MultiReader(bytes.NewReader(raw), r.Body), r.Body}
		return ""
	}
	_ = r.Body.Close()
	var fields map[string]json.RawMessage
	var id string
	if json.Unmarshal(raw, &fields) == nil {
		_ = json.Unmarshal(fields["task_id"], &id)
		id = strings.TrimSpace(id)
	}
	if room, bare := splitTag(id); room != "" {
		fields["task_id"], _ = json.Marshal(bare)
		if out, err := json.Marshal(fields); err == nil {
			raw = out
		}
	}
	r.Body = io.NopCloser(bytes.NewReader(raw))
	r.ContentLength = int64(len(raw))
	return id
}

// rememberedOn names a room that is not attached and that the hub last saw
// holding a card, or nothing. It only makes the 404 say where the card went.
func (p *Proxy) rememberedOn(bare string) string {
	stock := p.inventory()
	if stock == nil {
		return ""
	}
	names, err := stock.Holding()
	if err != nil {
		return ""
	}
	for _, name := range names {
		if p.hub.Has(name) {
			continue
		}
		held, err := stock.Remembered(name)
		if err != nil {
			continue
		}
		for _, c := range held {
			if c.ID == bare {
				return name
			}
		}
	}
	return ""
}

// cardUnplaced is the 404 for a card no attached room holds. It names the card
// and every room that was asked, so nobody is left reading "no rows".
func cardUnplaced(w http.ResponseWriter, bare string, asked []Attached, offline string) {
	names := make([]string, 0, len(asked))
	for _, a := range asked {
		names = append(names, a.Name)
	}
	msg := "card " + bare + " was not found on room " + strings.Join(names, ", room ")
	if offline != "" {
		msg += ". it was last seen on " + offline + ", which is not attached"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusNotFound)
	fmt.Fprintf(w, `{"error":%q}`, msg)
}
