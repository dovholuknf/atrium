package link

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"sync"
	"time"
)

// The hub-side enforcement of board-wide "approve everything".
//
// ── the seam, and why it is here ─────────────────────────
//
// A permission request blocks in the ROOM daemon: the hook POSTs it over
// loopback, `onPermRequest` runs the whole chain there, and if nothing in the
// chain answers it the request parks in `needs-permission` waiting for a human.
// The hub is a reverse proxy for the board (see proxy.go) and never sees that
// POST, so it cannot answer the request as it arrives.
//
// What the hub CAN see is the pending request, the same way the board does: over
// the relay, `GET /v1/permissions` on the room. And what the hub can do about it
// is exactly what a human clicking approve does: `POST /v1/permissions/<id>/decide`
// back down the same relay. So board-wide auto is enforced by watching each
// attached room for pending requests and answering them, with no room-side code
// and no room restart: it deploys on a hub restart alone.
//
// ── why this covers the three gap cases ──────────────────
//
// The old flag was a room setting read by the room's own gate, so it only
// covered sessions that gate had already registered, and in the ALL view it had
// no room to be written to at all. This does not register anything and does not
// care when a session started. It sweeps whatever a room reports as pending, so a
// freshly launched session, a reconnecting one and a session resuming during room
// startup are all answered the moment their request shows up in the room's list,
// which is the moment the room is attached. A room that attaches with a backlog
// of frozen requests has them swept as its stream comes up rather than waiting
// for a human to approve the first one.
//
// ── events, not a timer ──────────────────────────────────
//
// This used to sweep every attached room once a second while the switch was on,
// a GET per room per second whether or not anything was pending. It now WATCHES:
// while the switch is on the approver is an internal subscriber to the event feed
// for every room, which makes the feed run a pump per room even with no board
// open, and a `permission` event for a request nobody has decided is approved as
// it arrives. Cost while on is one open stream per room. Cost while off is
// nothing: no subscriber, so with no board open no pump runs, as before.
//
// A stream says what happens after it is up, so a FULL SWEEP of a room
// (`GET /v1/permissions`) still runs at the three moments a request could have
// been missed: the switch turning on, a room attaching, and that room's pump
// reconnecting. The last two are one thing, because a room reattaching drops its
// stream and the pump's reconnect is where the feed reports it.
//
// ── deciding at decision time ────────────────────────────
//
// Nothing about the switch is remembered from arrival. An event can sit queued
// while the switch is turned off, or run past its deadline, and approving it then
// would be the hub letting a request through that the human had already stopped
// letting through. So the switch AND its deadline are read again at the moment of
// approving, and nothing before that point counts.
//
// Decisions are by permission id. A sweep and an event can both find one request,
// and the second finds it already taken and does nothing, so a request is decided
// once here whichever path arrived first. The room refuses a repeat as well, but
// an approval that is only refused downstream is still a request made.
//
// ── what it does NOT override ────────────────────────────
//
// The chain still runs on the room first. A queued message, a shelved card and a
// standing `never` rule all decide a request inside `onPermRequest` before it can
// ever become pending, so anything this sees in `/v1/permissions` has already
// cleared them. Approving a pending request is therefore the same last-resort
// answer `docs/auto-mode.md` describes, made from the hub instead of the room.
//
// ── the record ───────────────────────────────────────────
//
// The decision is recorded twice, in two places, on purpose. The room writes it
// to that card's own history with the reason below, so the review shows what ran
// and why it was let through. The hub writes an operational line to its own audit
// log (see audit.go), because a board-wide decision is the hub's action and the
// hub is where "who turned the whole board loose" is answered.

// hubAutoReason is recorded against every request the hub's board-wide switch
// lets through, so a session told why it was allowed says which switch answered,
// and the review reads the same word.
const hubAutoReason = "board-wide auto mode: approved by the hub without asking, and recorded"

// hubAutoBy is who the room records as having decided. Without it the room files
// the decision under "you", which says a person clicked. The room accepts exactly
// this value and answers 400 to any other non-empty one, and an older room ignores
// the field and records "you" as it always did.
const hubAutoBy = "global-auto"

// decidedKeep is how long an id stays on the decided list. Long past the moment a
// sweep and an event for one request could both be in flight, and short enough
// that the list is never anything but a handful.
const decidedKeep = 10 * time.Minute

// autoApprover enforces the hub-wide auto-approve flag on the permission relay.
type autoApprover struct {
	p *Proxy

	once sync.Once
	// wake asks the loop to look at the switch now: it was just saved, on or off.
	// Turning it on empties the queue at once, which is what a person expects from
	// a button they just pressed. See `docs/auto-mode.md`, "turning it on empties
	// the queue".
	wake chan struct{}

	mu sync.Mutex
	// watching is the approver's own place in the event feed while the switch is
	// on, and nil while it is off. It is what keeps a pump running per room with
	// no board open.
	watching *sub
	// expiry re-looks at the switch when its deadline passes, so a switch left on
	// for an hour stops being watched after an hour with nobody pressing anything.
	expiry *time.Timer
	// decided is the permission ids this approver has taken, keyed by room and id,
	// with when. See `claim`.
	decided map[string]time.Time
}

func newAutoApprover(p *Proxy) *autoApprover {
	return &autoApprover{p: p, wake: make(chan struct{}, 1), decided: map[string]time.Time{}}
}

// start launches the loop once. Idempotent: SetInventory may be called more than
// once over a hub's life and only the first wiring starts the goroutine.
func (a *autoApprover) start() {
	a.once.Do(func() { go a.loop(context.Background()) })
}

// nudge asks the loop to look at the switch now.
func (a *autoApprover) nudge() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// loop looks at the switch when the hub starts, and again whenever it is nudged:
// saved through the board, or its deadline passing. No ticker.
func (a *autoApprover) loop(ctx context.Context) {
	for {
		a.evaluate(ctx)
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		}
	}
}

// state reads the switch and its deadline from the store, now.
//
// A read failure is not a licence to approve everything. The safe answer on the
// permission path is to gate, so it answers off, and the next look tries again.
// A deadline is checked against the clock here as well as in the store: this is
// the last thing standing between a request and an approval, so it does not rely
// on whichever Inventory it was handed to have checked.
func (a *autoApprover) state() (on bool, until *time.Time) {
	stock := a.p.inventory()
	if stock == nil {
		return false, nil
	}
	on, until, err := stock.BoardAuto()
	if err != nil || !on {
		return false, until
	}
	if until != nil && !time.Now().Before(*until) {
		return false, until
	}
	return true, until
}

// evaluate brings the approver in line with the switch: watching every room and
// having swept them when it is on, watching nothing when it is off.
func (a *autoApprover) evaluate(ctx context.Context) {
	on, until := a.state()
	if !on {
		a.unwatch()
		return
	}
	a.watch(until)
	// THE TURN-ON SWEEP, the first of the three moments a full sweep runs.
	// Anything already pending was raised before this was listening.
	a.sweepAll(ctx)
}

// watch subscribes to the event feed for every room, once, and arms the timer
// that ends the watch at the deadline.
func (a *autoApprover) watch(until *time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.expiry != nil {
		a.expiry.Stop()
		a.expiry = nil
	}
	if until != nil {
		a.expiry = time.AfterFunc(time.Until(*until), a.nudge)
	}
	if a.watching != nil {
		return
	}
	s := a.p.feeds.add("")
	a.watching = s
	go a.listen(s)
}

// unwatch leaves the event feed. The pumps stop with the last subscriber, so
// switching auto off with no board open stops every stream this started.
func (a *autoApprover) unwatch() {
	a.mu.Lock()
	s := a.watching
	a.watching = nil
	if a.expiry != nil {
		a.expiry.Stop()
		a.expiry = nil
	}
	a.mu.Unlock()
	if s != nil {
		a.p.feeds.drop(s)
	}
}

// listen reads the feed for permission events and approves the pending ones.
func (a *autoApprover) listen(s *sub) {
	for e := range s.ch {
		if e.Kind != "permission" {
			continue
		}
		// OFF THIS GOROUTINE, because approving is a request across a link and the
		// feed drops a subscriber whose channel fills. A slow room must not cost
		// the approver its seat.
		go a.onPermission(e)
	}
	// The channel closed. Either this approver left on purpose, which cleared
	// `watching` first, or the feed dropped it for being slow, in which case it
	// looks again: resubscribing, and sweeping for whatever went by.
	a.mu.Lock()
	dropped := a.watching == s
	if dropped {
		a.watching = nil
	}
	a.mu.Unlock()
	if dropped {
		a.nudge()
	}
}

// onPermission is one event. A request nobody has answered is approved. The
// same event says so again when it is decided, and that one is left alone.
func (a *autoApprover) onPermission(e Event) {
	var p struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
	}
	if err := json.Unmarshal(e.Data, &p); err != nil || p.ID == "" || p.Decision != "" {
		return
	}
	a.approveIfOn(context.Background(), e.Room, p.ID)
}

// sweepAll answers whatever every attached room is holding. The switch is read
// again for each request, not once for the sweep.
func (a *autoApprover) sweepAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, r := range a.p.hub.Rooms() {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			a.sweepRoom(ctx, name)
		}(r.Name)
	}
	wg.Wait()
}

// sweepRoomIfOn is the sweep a room's stream coming up asks for. With the switch
// off it reads the store and touches no room.
func (a *autoApprover) sweepRoomIfOn(ctx context.Context, room string) {
	if on, _ := a.state(); !on {
		return
	}
	a.sweepRoom(ctx, room)
}

// sweepRoom approves every pending request one room is holding.
func (a *autoApprover) sweepRoom(ctx context.Context, room string) {
	for _, id := range a.pendingIn(ctx, room) {
		a.approveIfOn(ctx, room, id)
	}
}

// approveIfOn is the only way a request gets approved here, from a sweep or from
// an event: the switch and its deadline as they are NOW, then the id.
func (a *autoApprover) approveIfOn(ctx context.Context, room, id string) {
	if on, _ := a.state(); !on {
		return
	}
	if !a.claim(room, id) {
		return
	}
	if !a.approve(ctx, room, id) {
		// The room never answered. Letting go of the id lets the next sweep try
		// again, which is the only thing that would.
		a.release(room, id)
	}
}

// claim reports whether this is the first attempt at a request, and takes it.
func (a *autoApprover) claim(room, id string) bool {
	key := room + "\x00" + id
	now := time.Now()
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.decided[key]; ok {
		return false
	}
	for k, at := range a.decided {
		if now.Sub(at) > decidedKeep {
			delete(a.decided, k)
		}
	}
	a.decided[key] = now
	return true
}

func (a *autoApprover) release(room, id string) {
	a.mu.Lock()
	delete(a.decided, room+"\x00"+id)
	a.mu.Unlock()
}

// pendingIn reads the ids of a room's pending requests. A room that cannot be
// reached contributes nothing, exactly as the aggregate fan-out treats a quiet
// room: it is skipped rather than failing the whole sweep.
func (a *autoApprover) pendingIn(ctx context.Context, room string) []string {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(rctx, http.MethodGet,
		"http://"+hostFor(room)+"/v1/permissions", nil)
	if err != nil {
		return nil
	}
	res, err := a.p.roomClient(room).Do(req)
	if err != nil {
		return nil
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
		return nil
	}
	var body struct {
		Permissions []struct {
			ID string `json:"id"`
		} `json:"permissions"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 8<<20)).Decode(&body); err != nil {
		return nil
	}
	out := make([]string, 0, len(body.Permissions))
	for _, p := range body.Permissions {
		if p.ID != "" {
			out = append(out, p.ID)
		}
	}
	return out
}

// approve answers one request. The same POST the board makes when a human clicks
// approve, so the room releases the agent through the one hop that can. A refusal
// is expected and ignored: the likely one is that this machine's own board
// answered it first, which comes back as a conflict.
//
// True when the room ANSWERED, whatever it said, and false when it could not be
// reached, which is the one case worth trying again.
func (a *autoApprover) approve(ctx context.Context, room, id string) bool {
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"decision": "approve", "reason": hubAutoReason, "by": hubAutoBy,
	})
	if err != nil {
		return true
	}
	req, err := http.NewRequestWithContext(rctx, http.MethodPost,
		"http://"+hostFor(room)+"/v1/permissions/"+url.PathEscape(id)+"/decide",
		bytes.NewReader(payload))
	if err != nil {
		return true
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.p.roomClient(room).Do(req)
	if err != nil {
		return false
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode == http.StatusOK {
		// The hub's own record of a board-wide decision. Best effort: the store's
		// log is fail-open and never blocks the relay, the same as everywhere
		// else the hub records one. See audit.go.
		a.p.RecordAudit(room, "permission", "board-wide auto approved "+id)
	}
	return true
}
