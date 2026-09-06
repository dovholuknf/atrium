package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// One board, many machines. The hub's side.
//
// `docs/federation-design-v2.md` settled the shape and this is stage one of it:
// LEAVES DIAL OUT AND THE FORUM HOLDS NOTHING DURABLE. A room connects to the
// hub, says what it is called and what is on it, and keeps saying so. The hub
// keeps that in memory and nowhere else.
//
// In memory is not a shortcut, it is the design. A room's cards are that room's
// state, held in that room's database, and a second durable copy here would be
// a second source of truth that is wrong whenever the room is unreachable. What
// this holds is a CACHE with a timestamp on it, and a room that stops talking
// goes stale and then disappears rather than persisting as a claim about a
// machine nobody can reach.
//
// WHAT A ROOM DOES NOT FEDERATE IS ATTACH. A pseudo terminal cannot leave the
// machine that made it: the daemon owns it, closing it takes the process with
// it, and ConPTY has no reattach. So a room contributes cards, their status,
// and what each is waiting for. Typing into a runner means pointing the board
// at that room directly, which is a dropdown rather than a rewrite because the
// board only ever holds a base URL per peer.

// roomStale is how long a room may go quiet before the board says so.
//
// Three missed heartbeats. Long enough that a slow link or a restart does not
// flap the list, short enough that a machine that has actually gone is not
// still being reported as present a minute later.
const roomStale = 3 * roomHeartbeat

// roomForget is when a quiet room stops being listed at all.
//
// It goes stale first and disappears later, because those answer different
// questions. Stale means "this was here and I cannot see it now", which is
// worth showing. Gone means nobody has heard from it in long enough that
// listing it is a claim rather than a memory.
const roomForget = 10 * time.Minute

// roomHeartbeat is how often a room is expected to check in. The room side
// uses the same constant, which is why it lives here rather than there.
const roomHeartbeat = 20 * time.Second

// RoomCard is one card on another machine, in the shape the hub keeps.
//
// A SUMMARY, not a Task. Copying the whole row would mean this file has to
// track every column the store grows, and the board only ever draws these on a
// list. Anything more detailed is a reason to open that room's own board.
type RoomCard struct {
	ID     string `json:"id"`
	Title  string `json:"title"`
	Status string `json:"status"`
	// Waiting is how long this card has wanted somebody, in seconds. The one
	// number that makes a remote card actionable rather than informational.
	Waiting int    `json:"waiting_seconds,omitempty"`
	Runner  string `json:"runner,omitempty"`
	Doing   string `json:"doing,omitempty"`
}

// RoomReport is what a room sends, every heartbeat.
type RoomReport struct {
	// Name is what this room calls itself. It is also the key, so two rooms
	// with one name are one room that flaps.
	Name string `json:"name"`
	// Board is where a browser should go to reach that room DIRECTLY, which is
	// the answer to everything this federation deliberately does not carry.
	// Empty when the room has no address worth handing out.
	Board string `json:"board,omitempty"`
	// Version and Host are for the operator looking at a list and asking which
	// machine that is.
	Version string     `json:"version,omitempty"`
	Host    string     `json:"host,omitempty"`
	Cards   []RoomCard `json:"cards"`
}

// Room is a room as the hub holds it: what it last said, and when.
type Room struct {
	RoomReport
	// Since is when this room first checked in, LastSeen when it last did.
	Since    time.Time `json:"-"`
	LastSeen time.Time `json:"-"`

	SinceRFC    string `json:"since"`
	LastSeenRFC string `json:"last_seen"`
	// Stale is whether it has missed enough heartbeats to be doubted. Computed
	// on read rather than stored, because it is a fact about now.
	Stale bool `json:"stale"`
	// Waiting is how many of its cards want somebody, so the board can say it
	// without walking the list.
	Waiting int `json:"waiting"`
}

// rooms is every room the hub has heard from. In memory, on purpose.
type rooms struct {
	mu  sync.Mutex
	all map[string]*Room
}

// Rooms is what the board draws, oldest first so the list does not reorder
// itself between polls.
func (d *Daemon) Rooms() any {
	d.rooms.mu.Lock()
	defer d.rooms.mu.Unlock()

	now := time.Now()
	out := make([]Room, 0, len(d.rooms.all))
	for name, r := range d.rooms.all {
		// Forgotten rather than listed. See `roomForget`.
		if now.Sub(r.LastSeen) > roomForget {
			delete(d.rooms.all, name)
			continue
		}
		c := *r
		c.Stale = now.Sub(r.LastSeen) > roomStale
		c.SinceRFC = r.Since.Format(time.RFC3339)
		c.LastSeenRFC = r.LastSeen.Format(time.RFC3339)
		c.Waiting = 0
		for _, card := range r.Cards {
			if card.Status == "needs-input" || card.Status == "needs-permission" {
				c.Waiting++
			}
		}
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return map[string]any{"rooms": out}
}

// RoomCheckIn records what a room just said.
//
// Every heartbeat REPLACES that room's cards rather than merging them. A merge
// would mean a card deleted on the room lives forever on the hub, and the whole
// point of holding nothing durable is that the room is the truth.
func (d *Daemon) RoomCheckIn(rep RoomReport) error {
	name := strings.TrimSpace(rep.Name)
	if name == "" {
		return fmt.Errorf("a room has to say what it is called")
	}
	if len(name) > 64 {
		return fmt.Errorf("that room name is too long to draw")
	}

	d.rooms.mu.Lock()
	defer d.rooms.mu.Unlock()
	if d.rooms.all == nil {
		d.rooms.all = map[string]*Room{}
	}
	now := time.Now()
	r, seen := d.rooms.all[name]
	if !seen {
		r = &Room{Since: now}
		d.rooms.all[name] = r
	}
	r.RoomReport = rep
	r.RoomReport.Name = name
	r.LastSeen = now
	if !seen {
		// Said once. A room checking in every twenty seconds is not news, and a
		// log line per heartbeat per room buries everything else.
		log.Printf("[atrium] room %q checked in from %s with %d card(s)",
			name, rep.Host, len(rep.Cards))
	}
	return nil
}

// handleRoomCheckIn is the endpoint a room posts to.
//
// ON THE HUMAN LISTENER rather than the agent one, which is worth saying. The
// agent listener closes when the store halts, so that a wedged atrium parks its
// runners instead of burning tokens. A room is not a runner: it is another
// operator surface, and a hub that has halted should still be able to say so to
// the rooms attached to it.
func (d *Daemon) handleRoomCheckIn(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	var rep RoomReport
	if err := json.Unmarshal(body, &rep); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if err := d.RoomCheckIn(rep); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	// The heartbeat interval comes back, so a room does not have to be
	// configured with something the hub already knows and the two cannot
	// drift.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "heartbeat_seconds": int(roomHeartbeat / time.Second),
	})
}
