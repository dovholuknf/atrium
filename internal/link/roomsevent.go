package link

import (
	"encoding/json"
	"sort"
	"time"
)

// What the `rooms` event says, and how the hub knows whether it changed.
//
// ── one builder, two readers ─────────────────────────────
//
// `/_hub/rooms`, `/_hub/inventory` and the `rooms` event are answers to the same
// two questions, and they are built by the SAME functions below. That is the
// whole defence against the event and the endpoints drifting: a board that paints
// from the event and a board that paints from a fetch after a reconnect are
// looking at one thing.
//
// ── the fingerprint ──────────────────────────────────────
//
// `rooms` is sent when the fingerprint of its payload differs from the last one
// sent, not when somebody knocked. Every change site nudges (`feeds.roomsChanged`)
// and the nudge does not say what changed, so a knock that changes nothing a
// board draws costs a store read and no event.
//
// WHAT IS LEFT OUT OF IT is what moves without meaning anything. `last_seen` on a
// record is rewritten on every heartbeat, and `idle` and `last_beat` on a live
// room move with the pool and the beat. Fingerprint those and `rooms` fires every
// five seconds forever, which is the poll this exists to remove wearing a
// different name. They are still IN the payload, as of the moment it was built.

// attachedView is what `/_hub/rooms` answers: the rooms answering right now, by
// name. Sorted, because the hub's map is not, and an order that changes between
// two reads would read as a change to the fingerprint.
func (p *Proxy) attachedView() []Attached {
	rooms := p.hub.Rooms()
	sort.Slice(rooms, func(i, j int) bool { return rooms[i].Name < rooms[j].Name })
	for i := range rooms {
		rooms[i].Setup = p.setupFor(rooms[i].Name)
	}
	return rooms
}

// inventoryView is what `/_hub/inventory` answers. `rooms` is nil, with `durable`
// false, on a hub that keeps no record, in which case the connection list stands
// in as it always did. The error is the store's, for the caller to decide about.
func (p *Proxy) inventoryView() (rooms any, durable bool, err error) {
	stock := p.inventory()
	if stock == nil {
		return p.attachedView(), false, nil
	}
	known, err := stock.Known()
	if err != nil {
		return nil, true, err
	}
	return known, true, nil
}

// roomsPayload builds the `rooms` event and the fingerprint that decides whether
// it goes out.
//
// A STORE THAT CANNOT BE READ leaves `inventory` and `durable` out rather than
// sending a lie or a stale list. The board treats a payload with no `attached`
// or no `inventory` as a cue to fetch, so it degrades to what it did before this
// event carried anything.
func (p *Proxy) roomsPayload() (payload []byte, fingerprint string) {
	attached := p.attachedView()
	names := make([]string, 0, len(attached))
	for _, r := range attached {
		names = append(names, r.Name)
	}
	body := map[string]any{"rooms": names, "only": p.hub.Only(), "attached": attached}

	// The fingerprint sees a copy with the volatile fields cleared.
	steady := make([]Attached, len(attached))
	for i, r := range attached {
		r.Idle = 0
		r.Beat = time.Time{}
		if r.Setup != nil {
			s := *r.Setup
			s.Checked = time.Time{}
			r.Setup = &s
		}
		steady[i] = r
	}
	calmed := map[string]any{"rooms": names, "only": body["only"], "attached": steady}

	inv, durable, err := p.inventoryView()
	if err == nil {
		body["inventory"], body["durable"] = inv, durable
		if known, ok := inv.([]Known); ok {
			calm := make([]Known, len(known))
			for i, k := range known {
				k.LastSeen = nil
				calm[i] = k
			}
			calmed["inventory"] = calm
		} else {
			calmed["inventory"] = steady
		}
		calmed["durable"] = durable
	}

	payload, _ = json.Marshal(body)
	fp, _ := json.Marshal(calmed)
	return payload, string(fp)
}

// RoomsChanged tells the hub something a board draws about a room has changed.
//
// For the places outside this package that know first: a join string spent, a
// room's `cleared_at` written by an announcement. It says nothing about what
// changed. The hub reads its own store and decides whether there is news.
func (p *Proxy) RoomsChanged() { p.feeds.roomsChanged() }
