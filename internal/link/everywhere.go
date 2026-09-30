package link

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// The everywhere index: the cards that carry the tag, on every room, so a bare
// name that misses on the caller's own room can be looked for on the others.
// See docs/rnd/everywhere-card-design.md.
//
// IN MEMORY AND NOTHING ELSE. It is derived from what each room last announced,
// so it is built from the cache when the hub starts and replaced for one room
// every time that room announces. The hub keeps no second list of which cards
// are on every room: the tag is the card's own, and a list here would go on
// naming a card after its room deleted it.

// EverywhereTag is the mark. Set the way any tag is set.
const EverywhereTag = "atrium:everywhere"

// everyCard is one indexed card. The payload is kept for a room that is not
// attached, which is the one time the cache is what gets shown.
type everyCard struct {
	Room    string
	ID      string
	Wire    string
	Alias   string
	Status  string
	Payload json.RawMessage
}

// spelled is the card as a caller should retype it, `handle@room (@alias)`.
func (c everyCard) spelled() string {
	name := c.Wire
	if name == "" {
		name = c.Alias
	}
	s := name + "@" + c.Room
	if c.Alias != "" && c.Wire != "" {
		s += " (@" + c.Alias + ")"
	}
	return s
}

// everywhere is the index, one slice of cards per room.
type everywhere struct {
	mu     sync.Mutex
	byRoom map[string][]everyCard
	// names keeps each room's spelling, since the map is keyed folded.
	names map[string]string
	// live is the rooms the hub still holds a record of, folded. Nil means all
	// of them. See `holding`.
	live func() map[string]bool
}

func newEverywhere() *everywhere {
	return &everywhere{byRoom: map[string][]everyCard{}, names: map[string]string{}}
}

// indexed reads the cards of one announcement that belong in the index: the
// tag, and not `done` or `dead`. The status rule is the one alias resolution
// uses, an ended card keeps its tag as a record and no longer answers to it.
func indexed(room string, cards []CardState) []everyCard {
	var out []everyCard
	for _, c := range cards {
		if !cardLive(c.Status) {
			continue
		}
		var row struct {
			Wire  string   `json:"wire_name"`
			Alias string   `json:"alias"`
			Tags  []string `json:"tags"`
		}
		if err := json.Unmarshal(c.Payload, &row); err != nil {
			continue
		}
		tagged := false
		for _, t := range row.Tags {
			if equalFold(strings.TrimSpace(t), EverywhereTag) {
				tagged = true
				break
			}
		}
		if !tagged {
			continue
		}
		out = append(out, everyCard{Room: room, ID: c.ID, Wire: row.Wire,
			Alias:  lowerASCII(strings.TrimPrefix(strings.TrimSpace(row.Alias), "@")),
			Status: c.Status, Payload: c.Payload})
	}
	return out
}

// replace swaps one room's cards for what it just announced, and says whether
// that changed the index.
func (e *everywhere) replace(room string, cards []CardState) bool {
	next := indexed(room, cards)
	key := keyOf(room)
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.byRoom[key]
	if len(next) == 0 {
		delete(e.byRoom, key)
		delete(e.names, key)
		return len(prev) > 0
	}
	e.byRoom[key], e.names[key] = next, room
	if len(prev) != len(next) {
		return true
	}
	for i := range next {
		a, b := prev[i], next[i]
		if a.ID != b.ID || a.Wire != b.Wire || a.Alias != b.Alias || a.Status != b.Status ||
			string(a.Payload) != string(b.Payload) {
			return true
		}
	}
	return false
}

// all is every indexed card except those on `besides`, in room then id order.
//
// A ROOM THE HUB NO LONGER HOLDS IS LEFT OUT. Removing or forcing a room out
// cascades its cached cards away, and that can be done by the rooms CLI in
// another process, which this one cannot be told about. So the index is checked
// against what the store still holds, and a failed check leaves it as it is.
func (e *everywhere) all(besides string) []everyCard {
	e.mu.Lock()
	live := e.live
	e.mu.Unlock()
	var held map[string]bool
	if live != nil {
		held = live()
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	var out []everyCard
	for key, cards := range e.byRoom {
		if key == keyOf(besides) || (held != nil && !held[key]) {
			continue
		}
		out = append(out, cards...)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Room != out[j].Room {
			return keyOf(out[i].Room) < keyOf(out[j].Room)
		}
		return out[i].ID < out[j].ID
	})
	return out
}

// drop forgets one room's cards, for a room whose record has just gone.
func (e *everywhere) drop(room string) bool {
	e.mu.Lock()
	defer e.mu.Unlock()
	key := keyOf(room)
	_, had := e.byRoom[key]
	delete(e.byRoom, key)
	delete(e.names, key)
	return had
}

// find is the cards on other rooms that answer to `who`: a handle, or an alias,
// with case and a leading `@` ignored. Never a card id, since another room's
// card is `room~id` and that already routes.
func (e *everywhere) find(besides, who string) []everyCard {
	who = strings.TrimSpace(who)
	bare := strings.TrimPrefix(who, "@")
	if bare == "" {
		return nil
	}
	var out []everyCard
	for _, c := range e.all(besides) {
		if c.answersTo(bare) {
			out = append(out, c)
		}
	}
	return out
}

// answersTo is whether `name` is this card's handle, its handle without the
// atrium's prefix, or its alias.
func (c everyCard) answersTo(name string) bool {
	if c.Wire != "" {
		if equalFold(c.Wire, name) {
			return true
		}
		if i := strings.LastIndex(c.Wire, "/"); i >= 0 && equalFold(c.Wire[i+1:], name) {
			return true
		}
	}
	return c.Alias != "" && c.Alias == lowerASCII(name)
}

// IndexEverywhere replaces one room's cards in the everywhere index. Called
// with what the room announced, and once per room from the cache when the hub
// starts. Reports whether the index changed.
func (h *Hub) IndexEverywhere(room string, cards []CardState) bool {
	return h.every.replace(room, cards)
}

// lookupEverywhere is the one card on another room that `who` names, or the
// refusal that says why not: a 409 naming every match when two or more rooms
// answer, a 404 carrying the everywhere list when none does. `code` is the
// refusal's status, zero on a match.
func (h *Hub) lookupEverywhere(besides, who string) (card everyCard, code int, err error) {
	if h == nil {
		return everyCard{}, http.StatusNotFound, fmt.Errorf("no card called %q on another room", who)
	}
	switch found := h.every.find(besides, who); len(found) {
	case 1:
		return found[0], 0, nil
	case 0:
		msg := fmt.Sprintf("no card called %q on another room", who)
		var list []string
		for _, c := range h.every.all(besides) {
			list = append(list, c.spelled())
		}
		if len(list) > 0 {
			msg += ". cards on every room: " + strings.Join(list, ", ")
		}
		return everyCard{}, http.StatusNotFound, fmt.Errorf("%s", msg)
	default:
		var list []string
		for _, c := range found {
			list = append(list, c.spelled())
		}
		return everyCard{}, http.StatusConflict, fmt.Errorf("%q names a card on more than one room: %s. "+
			"say which, as name@room", strings.TrimPrefix(strings.TrimSpace(who), "@"), strings.Join(list, ", "))
	}
}

// sendName is what a say is addressed with on the card's own room.
func (c everyCard) sendName() string {
	if c.Wire != "" {
		return c.Wire
	}
	return c.Alias
}

// everywhereFallthrough is what a tool that may reach another room does when a
// bare name missed on the caller's own room, `miss` being that miss. One match
// comes back. Two or more is the 409 sentence. None is the miss itself with
// the everywhere cards added, as `handle@room`, so the caller sees them too.
//
// A miss that is the room not answering is not a miss, and a caller with no room
// reads the aggregate list, where every room is already looked at.
func (c *controlMCP) everywhereFallthrough(room, who string, miss error) (everyCard, error) {
	var be *boardError
	if room == "" || c.hub == nil || errors.As(miss, &be) {
		return everyCard{}, miss
	}
	card, code, err := c.hub.lookupEverywhere(room, who)
	switch code {
	case 0:
		return card, nil
	case http.StatusConflict:
		return everyCard{}, err
	}
	var list []string
	for _, e := range c.hub.every.all(room) {
		list = append(list, e.spelled())
	}
	if len(list) == 0 {
		return everyCard{}, miss
	}
	return everyCard{}, fmt.Errorf("%s. on other rooms: %s", miss.Error(), strings.Join(list, ", "))
}

// asPeer is the card as a row of atrium_peers.
func (c everyCard) asPeer() peer {
	return peer{Handle: c.sendName() + "@" + c.Room, Card: tagFor(c.Room, c.ID), Room: c.Room,
		Status: c.Status, Everywhere: true}
}

// setHolding ties the index to the store: a room counts only while the store
// still holds cards for it. Nil stops checking.
func (e *everywhere) setHolding(s Inventory) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if s == nil {
		e.live = nil
		return
	}
	e.live = func() map[string]bool {
		names, err := s.Holding()
		if err != nil {
			return nil
		}
		held := make(map[string]bool, len(names))
		for _, n := range names {
			held[keyOf(n)] = true
		}
		return held
	}
}
