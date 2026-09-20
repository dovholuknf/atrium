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
// of frozen requests has them swept on the next tick rather than waiting for a
// human to approve the first one.
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

// autoSweepEvery is how often the approver checks each attached room while the
// flag is on. A second matches the event feed's cadence (events.go) and is
// invisible next to the wait a human would otherwise be: nobody is watching a
// board every second, so this is still "before it reaches a person". When the
// flag is off the tick costs one cheap flag read and touches no room.
const autoSweepEvery = time.Second

// autoApprover enforces the hub-wide auto-approve flag on the permission relay.
type autoApprover struct {
	p *Proxy

	once sync.Once
	// wake shortcuts the tick so turning the switch on empties the queue at once
	// rather than on the next second, which is what a person expects from a
	// button they just pressed. See `docs/auto-mode.md`, "turning it on empties
	// the queue".
	wake chan struct{}
}

func newAutoApprover(p *Proxy) *autoApprover {
	return &autoApprover{p: p, wake: make(chan struct{}, 1)}
}

// start launches the loop once. Idempotent: SetInventory may be called more than
// once over a hub's life and only the first wiring starts the goroutine.
func (a *autoApprover) start() {
	a.once.Do(func() { go a.loop(context.Background()) })
}

// nudge asks the loop to sweep now rather than wait out the tick.
func (a *autoApprover) nudge() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

func (a *autoApprover) loop(ctx context.Context) {
	t := time.NewTicker(autoSweepEvery)
	defer t.Stop()
	for {
		a.sweepIfOn(ctx)
		select {
		case <-ctx.Done():
			return
		case <-a.wake:
		case <-t.C:
		}
	}
}

// sweepIfOn reads the flag and, when it is on, answers every attached room's
// pending requests.
func (a *autoApprover) sweepIfOn(ctx context.Context) {
	stock := a.p.inventory()
	if stock == nil {
		return
	}
	on, _, err := stock.BoardAuto()
	if err != nil {
		// A read failure is not a licence to approve everything. The safe answer
		// on the permission path is to gate, so this sweep does nothing and the
		// next one tries again.
		return
	}
	if !on {
		return
	}
	rooms := a.p.hub.Rooms()
	var wg sync.WaitGroup
	for _, r := range rooms {
		wg.Add(1)
		go func(name string) {
			defer wg.Done()
			a.sweepRoom(ctx, name)
		}(r.Name)
	}
	wg.Wait()
}

// sweepRoom approves every pending request one room is holding.
func (a *autoApprover) sweepRoom(ctx context.Context, room string) {
	for _, id := range a.pendingIn(ctx, room) {
		a.approve(ctx, room, id)
	}
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
func (a *autoApprover) approve(ctx context.Context, room, id string) {
	rctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	payload, err := json.Marshal(map[string]any{
		"decision": "approve", "reason": hubAutoReason,
	})
	if err != nil {
		return
	}
	req, err := http.NewRequestWithContext(rctx, http.MethodPost,
		"http://"+hostFor(room)+"/v1/permissions/"+url.PathEscape(id)+"/decide",
		bytes.NewReader(payload))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := a.p.roomClient(room).Do(req)
	if err != nil {
		return
	}
	defer res.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(res.Body, 1<<16))
	if res.StatusCode == http.StatusOK {
		// The hub's own record of a board-wide decision. Best effort: the store's
		// log is fail-open and never blocks the relay, the same as everywhere
		// else the hub records one. See audit.go.
		a.p.RecordAudit(room, "permission", "board-wide auto approved "+id)
	}
}
