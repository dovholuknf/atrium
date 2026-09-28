package link

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
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
var notCards = map[string]bool{"prune": true, "pin-order": true}

// cardRoomKey carries the room the named card was placed on, ahead of every
// other way `roomFor` has of choosing one.
type cardRoomKey struct{}

// placeCard routes a request that names a card to the room holding it.
//
// It returns the request to carry on with, or false once it has answered.
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
	} else {
		rooms := p.hub.Rooms()
		if len(rooms) < 2 {
			// One room or none is nothing to choose between. The dial goes to the
			// only room, or says none is attached.
			return r, true
		}
		owner, ok := p.roomHolding(r, bare, rooms)
		if !ok {
			cardUnplaced(w, bare, rooms, p.rememberedOn(bare))
			return r, false
		}
		room = owner
	}
	return r.WithContext(context.WithValue(r.Context(), cardRoomKey{}, room)), true
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
