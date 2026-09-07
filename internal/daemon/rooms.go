package daemon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
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

// roomBusyHeartbeat is how often a room checks in while somebody on it is
// frozen waiting for an answer.
//
// The check-in is the only channel that goes both ways: a room dials out, so a
// decision made here rides back on the answer to the next check-in and nothing
// else can carry it. At the ordinary heartbeat that means up to twenty seconds
// between pressing approve and the agent moving, which reads as a board that
// did not do anything. So a room that reported a pending request checks in
// faster, and only while it has one.
//
// Two seconds rather than one. A blocked agent is already waiting on a human,
// so the cost of a second is nothing next to the cost of a room hammering a
// hub it reaches over an overlay.
const roomBusyHeartbeat = 2 * time.Second

// roomDecisionTTL is how long an answer stays queued for a room before it is
// given up on.
//
// Nothing here acknowledges. The hub hands a decision over on a check-in and
// the room applies it to its OWN daemon, which may refuse it: the request may
// have been answered on that machine a moment earlier, or the card shelved, or
// that daemon's store halted. An entry that has been handed over and has not
// made the request go away is therefore not proof of anything, so it expires
// and the request becomes answerable from this board again.
//
// The failure this avoids is the worse one. A decision that hid a request
// forever would leave an agent frozen on another machine with nothing on any
// board to say so, which is the exact thing this feature exists to stop.
const roomDecisionTTL = 2 * time.Minute

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

// RoomPerm is one permission request on another machine, as that machine
// reports it.
//
// The fields are the ones the board needs to DECIDE: which tool, what command,
// who is asking, what changes, and how long they have been stuck. Not the
// store row: dedup keys, rule provenance and the decision columns are that
// machine's bookkeeping and mean nothing here.
//
// THE CHANNEL DOES NOT MOVE. `HandlePermission` on the room blocks on a channel
// held in that room's process. This is a description of that request, so it can
// be drawn and answered here, and the answer travels back for the room's own
// handler to unblock its own channel. Nothing on this side can release an
// agent on that side, and nothing here should look like it can.
type RoomPerm struct {
	ID     string `json:"id"`
	TaskID string `json:"task_id,omitempty"`
	Tool   string `json:"tool"`
	// Command is what would run, and is editable here for the same reason it is
	// editable on the room's own board: approving the wrong command is worse
	// than refusing it.
	Command string `json:"command"`
	// Agent is the card's display title, resolved by the room. The hub has no
	// way to turn a task id into a name, and would draw "an agent" without it.
	Agent string `json:"agent,omitempty"`
	// Details is what actually changes, the diff for an edit or the content for
	// a write. Truncated by the room, since a report has to fit in one POST.
	Details string `json:"details,omitempty"`
	// Waiting is how long the agent has been frozen, in seconds, MEASURED ON
	// THE ROOM'S CLOCK. A timestamp would be read against this machine's clock
	// and two machines that disagree by a minute would draw a request as
	// frozen for a minute before it was made, or as brand new when it is old.
	// The same reason `wait_seconds` on a card is seconds and not a time.
	Waiting int `json:"waiting_seconds"`
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
	// Perms is every request on that machine still waiting for a human.
	// Replaced on every check-in, like the cards and for the same reason.
	Perms []RoomPerm `json:"permissions,omitempty"`

	// Launches is whether this room will start work the hub queues for it.
	//
	// THE ROOM DECIDES, and it says so on every check-in rather than being
	// configured here. Reporting cards and accepting processes are two
	// different amounts of trust, and the machine granting the second one is
	// the one that should be able to withdraw it by restarting with a flag.
	//
	// A room that says no is handed nothing, so its queue sits visible on the
	// board instead of being spent on refusals. See `dispatch.go`.
	Launches bool `json:"launches,omitempty"`
	// Busy is that room starting things right now, which is a different fact
	// from not taking work at all and is drawn differently.
	//
	// Kept apart from `Launches` for the board's sake rather than the hub's:
	// both mean "hand me nothing this time round", and a room that says the
	// standing no should not read as one that is merely mid-batch.
	Busy bool `json:"busy,omitempty"`
	// Workspace is the directory a queued launch may name on this room, when
	// the room has one. Drawn on the board so the operator queueing work can
	// see what a directory will be checked against instead of finding out from
	// a refusal.
	Workspace string `json:"workspace,omitempty"`
}

// RoomRequest is one remote request as the board reads it.
//
// It carries the room's name because a decision has to be addressed, and a
// timestamp because every existing piece of the board that draws a request
// reads `requested_at`. The timestamp is computed HERE, from the seconds the
// room reported, so it is expressed in the clock the browser is about to
// compare it against.
type RoomRequest struct {
	RoomPerm
	Room        string `json:"room"`
	RequestedAt string `json:"requested_at"`
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
	// Requests is what this room is asking a human for, in the shape the board
	// draws. Filled on read from the last report, minus anything already
	// answered from here and not yet gone.
	Requests []RoomRequest `json:"requests"`

	// ForgetIn is how many seconds this room has left before it stops being
	// listed at all. See `roomForget`.
	//
	// Here so that a stale room can say what is about to happen to it. Stale
	// on its own is a state; stale with a deadline is the difference between
	// "something is wrong" and "wait, or go and look at that machine", and the
	// board should not be recomputing this from a constant it has its own copy
	// of.
	ForgetIn int `json:"forget_in_seconds"`

	// answers is what this board has decided and the room has not yet acted
	// on, keyed by the request id. Unexported: it is one side of a handover in
	// flight, not a fact about the room worth publishing.
	answers map[string]*roomAnswer
}

// roomAnswer is a decision waiting to be collected, and what has happened to
// it so far.
type roomAnswer struct {
	dec RoomDecision
	// at is when it was decided, which is when the clock on `roomDecisionTTL`
	// starts.
	at time.Time
	// sent is whether a check-in has already carried it away. It stays here
	// after that, so the request is not drawn as unanswered in the seconds
	// between the room taking the decision and the room's next report no
	// longer mentioning it. Without this, one approval is offered twice.
	sent bool
}

// RoomDecision is an answer going the other way: from this board to the room
// that holds the blocked channel.
//
// The fields are the body of the room's own `POST /v1/permissions/{id}/decide`,
// because that is what the room does with it. Forever, prefix and kind ride
// along so "always" and "never" work on a remote request, and the rule they
// create is written in THAT machine's rule table, which is the only place it
// could mean anything: the rule is consulted by the daemon that will be asked
// again.
type RoomDecision struct {
	Perm     string `json:"permission_id"`
	Decision string `json:"decision"`
	Reason   string `json:"reason,omitempty"`
	Command  string `json:"command,omitempty"`
	Forever  bool   `json:"forever,omitempty"`
	Prefix   string `json:"prefix,omitempty"`
	Kind     string `json:"kind,omitempty"`
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
		quiet := now.Sub(r.LastSeen)
		c.Stale = quiet > roomStale
		c.ForgetIn = int((roomForget - quiet) / time.Second)
		if c.ForgetIn < 0 {
			c.ForgetIn = 0
		}
		c.SinceRFC = r.Since.Format(time.RFC3339)
		c.LastSeenRFC = r.LastSeen.Format(time.RFC3339)
		c.Waiting = 0
		for _, card := range r.Cards {
			if card.Status == "needs-input" || card.Status == "needs-permission" {
				c.Waiting++
			}
		}
		// Anything decided here and gone through is dropped before the list is
		// built, so a request the room has stopped reporting cannot come back.
		r.settle(now)
		c.Requests = make([]RoomRequest, 0, len(r.Perms))
		for _, p := range r.Perms {
			// A request with an answer waiting for it is NOT offered again.
			// Two approvals for one request is either a conflict on the room
			// or, if the room applied both, the second one running the command
			// a second time.
			if r.answers[p.ID] != nil {
				continue
			}
			if p.Waiting < 0 {
				// A room whose clock ran backwards said something impossible.
				// Nothing sensible can be drawn from it, so it becomes "asked
				// just now" rather than a time in the future.
				p.Waiting = 0
			}
			c.Requests = append(c.Requests, RoomRequest{
				RoomPerm: p, Room: name,
				RequestedAt: now.Add(-time.Duration(p.Waiting) * time.Second).
					UTC().Format(time.RFC3339),
			})
		}
		// The report's own copy is not published twice, and the answers in
		// flight are not published at all.
		c.Perms = nil
		c.answers = nil
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return map[string]any{"rooms": out}
}

// roomPermsMax is how many pending requests one report may carry.
//
// The room bounds this before it sends, so this is the guard against a report
// that did not. The cost of not having it is a browser asked to draw a thousand
// permission cards, each with a text box in it, which is a board that stops
// responding rather than a board that is wrong.
const roomPermsMax = 50

// roomDetailsMax is how much of a diff one request may carry.
//
// Also bounded by the room, and for a different reason than the count: a whole
// report has to fit in one POST, and the hub reads at most a megabyte of it. A
// single large diff that pushed the report over that limit would fail the
// CHECK-IN, which would take the cards down with it and make the room look
// gone. Truncated detail is worth more than a room that disappears when one of
// its agents edits a big file.
const roomDetailsMax = 4000

// errRoomStaleDecision is answering something this hub can no longer address:
// the room has gone, or it has stopped asking, or it has already been answered
// from here. All three mean "you are looking at something that has moved on",
// which is the same thing the local queue says with a 409.
var errRoomStaleDecision = errors.New("that request has moved on")

// RoomCheckIn records what a room just said, and hands back what this board
// has decided for it.
//
// Every heartbeat REPLACES that room's cards rather than merging them. A merge
// would mean a card deleted on the room lives forever on the hub, and the whole
// point of holding nothing durable is that the room is the truth.
//
// THE RETURN VALUE IS THE ONLY WAY A DECISION TRAVELS. A room dials out, so
// nothing here can dial back into it, and the request's channel is held in that
// room's process and cannot move. So an answer waits until the room next says
// hello and leaves on the reply.
func (d *Daemon) RoomCheckIn(rep RoomReport) ([]RoomDecision, error) {
	name := strings.TrimSpace(rep.Name)
	if name == "" {
		return nil, fmt.Errorf("a room has to say what it is called")
	}
	if len(name) > 64 {
		return nil, fmt.Errorf("that room name is too long to draw")
	}
	if len(rep.Perms) > roomPermsMax {
		rep.Perms = rep.Perms[:roomPermsMax]
	}
	for i := range rep.Perms {
		if len(rep.Perms[i].Details) > roomDetailsMax {
			rep.Perms[i].Details = rep.Perms[i].Details[:roomDetailsMax]
		}
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

	// Settled against the report that just arrived, so a decision the room has
	// already acted on is dropped before it can be handed over twice.
	r.settle(now)
	out := make([]RoomDecision, 0, len(r.answers))
	for _, a := range r.answers {
		a.sent = true
		out = append(out, a.dec)
	}
	// Ordered, so a room applying a batch does it the same way every time and a
	// test can say what it expects.
	sort.Slice(out, func(i, j int) bool { return out[i].Perm < out[j].Perm })
	return out, nil
}

// settle drops answers that have nothing left to do.
//
// Called with the report just received in place, because "nothing left to do"
// is a question about what the room is still asking for.
func (r *Room) settle(now time.Time) {
	if len(r.answers) == 0 {
		return
	}
	asking := make(map[string]bool, len(r.Perms))
	for _, p := range r.Perms {
		asking[p.ID] = true
	}
	for id, a := range r.answers {
		// The room has stopped asking. Either it took this decision or it
		// answered the request some other way, and either way the agent is
		// running again and there is nothing here to deliver.
		if !asking[id] {
			delete(r.answers, id)
			continue
		}
		// Still being asked long after being answered, so the answer did not
		// take. See `roomDecisionTTL`: the request goes back to being
		// answerable rather than staying hidden behind a decision that failed.
		if now.Sub(a.at) > roomDecisionTTL {
			log.Printf("[atrium] room %q is still asking about %s %s after being told %q, "+
				"so it is being asked again", r.Name, a.dec.Perm, roomDecisionTTL, a.dec.Decision)
			delete(r.answers, id)
		}
	}
}

// RoomDecide answers a request that is blocking an agent on another machine.
//
// It QUEUES, and that is the whole of what this hub does about it. The room
// collects the decision on its next check-in and posts it to its own daemon,
// which unblocks its own channel. Nothing here touches an agent and nothing
// here records the outcome: the decision, the rule it may create and the
// history it lands in all belong to the machine that was asked.
func (d *Daemon) RoomDecide(room, perm string, dec RoomDecision) error {
	room = strings.TrimSpace(room)
	perm = strings.TrimSpace(perm)
	if dec.Decision != "approve" && dec.Decision != "block" {
		return fmt.Errorf("a decision is approve or block")
	}
	if dec.Forever && strings.TrimSpace(dec.Prefix) == "" {
		// Refused here rather than sent on. A standing answer with no scope is
		// a rule that matches everything, and finding that out on the other
		// machine means finding out after it was written.
		return fmt.Errorf("a standing answer needs the scope it covers")
	}
	dec.Perm = perm

	d.rooms.mu.Lock()
	defer d.rooms.mu.Unlock()
	r := d.rooms.all[room]
	if r == nil {
		return fmt.Errorf("%w: no room called %q is reporting in", errRoomStaleDecision, room)
	}
	asking := false
	for _, p := range r.Perms {
		if p.ID == perm {
			asking = true
			break
		}
	}
	if !asking {
		return fmt.Errorf("%w: %s is not asking for that any more, so it was answered there",
			errRoomStaleDecision, room)
	}
	if a := r.answers[perm]; a != nil {
		return fmt.Errorf("%w: that was already answered with %s, and %s has not taken it yet",
			errRoomStaleDecision, a.dec.Decision, room)
	}
	if r.answers == nil {
		r.answers = map[string]*roomAnswer{}
	}
	r.answers[perm] = &roomAnswer{dec: dec, at: time.Now()}
	log.Printf("[atrium] %s for a request on room %q, waiting for it to be collected",
		dec.Decision, room)
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
	// The stored copy is keyed on the trimmed name, so the handout has to look
	// work up under the same one. Two spellings of one room is two queues, and
	// only one of them is ever drawn.
	rep.Name = strings.TrimSpace(rep.Name)
	decisions, err := d.RoomCheckIn(rep)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	// WORK RIDES THE REPLY TOO. This is the entire outward path: the hub never
	// dials a room, so anything it wants to say has to travel back down a
	// connection the room already made. See `dispatch.go`.
	//
	// After the check-in is recorded, not before, because the handout is
	// decided from what this report just said about itself.
	launches := d.handoutFor(rep)
	if launches == nil {
		launches = []dispatchHandout{}
	}
	// The heartbeat intervals come back, so a room does not have to be
	// configured with something the hub already knows and the two cannot
	// drift. Both of them: the busy one is what a room uses while it is
	// carrying a request, and it is this side that decides how hard it is
	// willing to be asked.
	//
	// The decisions ride out on the same answer, because there is no other
	// direction available. See `RoomCheckIn`.
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok":                     true,
		"heartbeat_seconds":      int(roomHeartbeat / time.Second),
		"busy_heartbeat_seconds": int(roomBusyHeartbeat / time.Second),
		"decisions":              decisions,
		"launches":               launches,
	})
}

// handleRoomDecide is the board answering a request on another machine.
//
// ON THE HUMAN LISTENER, beside the rest of the rooms endpoints, and for the
// same reason: this is an operator surface. It is also the reason the answer
// says nothing about whether the agent moved. It cannot know. All it can report
// is that the decision is queued for a room that is currently reporting in, and
// the request disappearing from the board on the next poll is what says the
// room took it.
func (d *Daemon) handleRoomDecide(w http.ResponseWriter, r *http.Request) {
	var dec RoomDecision
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&dec); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	room, perm := r.PathValue("room"), r.PathValue("id")
	if err := d.RoomDecide(room, perm, dec); err != nil {
		// Answering something that has moved on is the same event the local
		// queue reports with a 409, and it reads the same way on the board: a
		// button that was drawn from a poll one moment out of date.
		if errors.Is(err, errRoomStaleDecision) {
			writeJSONErr(w, http.StatusConflict, err)
			return
		}
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "queued_for": room,
		"collected_within_seconds": int(roomBusyHeartbeat / time.Second),
	})
}

// RoomForget drops a room from the list on purpose.
//
// IT IS NOT A BLOCK LIST, and it deliberately writes nothing. The hub holds
// what it has been told and nothing else, so forgetting is the same operation
// time performs at `roomForget`, done early. A machine that is still running
// `atrium room` reappears on its next heartbeat, and that is the correct
// outcome: the alternative is a durable record of a refusal, which is the
// second source of truth this whole design exists to not have.
//
// What it is FOR is the machine that has gone. A laptop that was reimaged, a
// room renamed, a test run that left a name behind: those sit stale for ten
// minutes saying something nobody needs to read.
func (d *Daemon) RoomForget(name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, fmt.Errorf("say which room to forget")
	}
	d.rooms.mu.Lock()
	defer d.rooms.mu.Unlock()
	if _, ok := d.rooms.all[name]; !ok {
		return false, nil
	}
	delete(d.rooms.all, name)
	log.Printf("[atrium] room %q forgotten from the board", name)
	return true, nil
}

// handleRoomForget is the endpoint the board presses.
func (d *Daemon) handleRoomForget(w http.ResponseWriter, r *http.Request) {
	had, err := d.RoomForget(r.PathValue("name"))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	// A room that was not there is not an error. Two presses, or a room that
	// aged out between the draw and the click, mean the same thing to whoever
	// pressed it: that room is not on the board now.
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "was_listed": had})
}

// RoomJoinInfo is what the board needs to write the command that gets run on
// the OTHER machine.
//
// READ, NEVER STORED. There is no such thing as a pending room here: nothing
// is created by looking at this, and a room exists exactly when it checks in.
// Every field is either a constant or something this hub already knows about
// itself, so the dialog over it is a text generator and not a form that saves.
type RoomJoinInfo struct {
	// Heartbeat, Stale and Forget are the timings, so the board can say what
	// stale means in seconds rather than repeating the numbers in the page and
	// drifting from them.
	Heartbeat int `json:"heartbeat_seconds"`
	Stale     int `json:"stale_seconds"`
	Forget    int `json:"forget_seconds"`

	// Service is the ziti service this hub answers on, when one is configured.
	// It is what goes in `--service`, and it is the interesting case, because
	// neither machine has to be reachable from the other.
	Service string `json:"service,omitempty"`
	// ServiceRunning is whether that listener is actually up. A configured
	// service nobody is serving is a command that fails, and the board should
	// say so rather than hand it over.
	ServiceRunning bool `json:"service_running"`

	// Share is the zrok address, when the board is being shared publicly. A
	// private share is a token rather than a URL and a room cannot post to it,
	// so this stays empty in that case.
	Share string `json:"share,omitempty"`

	// Address is an ordinary URL for `--hub`, built from this machine's
	// hostname and the port the board listens on. It works on a network where
	// the room can already reach this machine and nowhere else, which is why
	// the board offers it last and says so.
	Address string `json:"address,omitempty"`
	// Local is what this daemon is actually listening on, loopback and all.
	// Shown so somebody who knows their own network can see what to correct
	// Address to.
	Local string `json:"local"`
}

// RoomJoin answers GET /v1/rooms/join.
func (d *Daemon) RoomJoin() any {
	z := d.zitiConfig()
	info := RoomJoinInfo{
		Heartbeat:      int(roomHeartbeat / time.Second),
		Stale:          int(roomStale / time.Second),
		Forget:         int(roomForget / time.Second),
		Service:        strings.TrimSpace(z.Service),
		ServiceRunning: d.nat(OverlayZiti).running(),
		Local:          d.defaultBackend(),
	}
	// Only a public share has an address a room could post to. A private one
	// is a token, and offering that as a hub URL produces a room that retries
	// forever against something that was never a URL.
	if zr := d.nat(OverlayZrok); zr.running() {
		if addr := zr.state("").Address; strings.HasPrefix(addr, "http") {
			info.Share = addr
		}
	}
	if host, err := os.Hostname(); err == nil && strings.TrimSpace(host) != "" {
		if _, port, splitErr := net.SplitHostPort(d.opts.HumanAddr); splitErr == nil && port != "" {
			info.Address = "http://" + host + ":" + port
		}
	}
	return info
}
