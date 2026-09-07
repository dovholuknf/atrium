package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Sending work the other way.
//
// `rooms.go` is the inward half: a room says what is on it and the hub holds
// that in memory. This is the outward half, and the whole of its design is that
// IT ADDS NO CONNECTION. The check-in already happens every twenty seconds and
// already gets a reply, so a queued launch rides the reply. Nothing dials a
// room, nothing has to be reachable, and a leaf on a coffee shop network or a
// cloud box behind a security group is not a case anybody configures for.
//
// The queue itself is durable and lives in `internal/store/dispatch.go`, which
// carries the reasoning for the row shape and the claim.

// DispatchKeepSettled is how long a finished queue item stays on the board.
//
// A DAY, and the long end of the choice on purpose. A failed item IS the
// product here: it is the readable reason a launch did not happen on a machine
// nobody was watching, and clearing it after an hour would mean coming back in
// the morning to a queue that looks like nothing was ever wrong. An item that
// started is kept for the same length only because two rules is worse than one.
const DispatchKeepSettled = 24 * time.Hour

// dispatchHandout is what a room is told to start, in the reply to its
// check-in.
//
// A SEPARATE SHAPE FROM `store.Dispatch` on purpose. What crosses is an
// instruction, not a row: no token bookkeeping the room has no use for, no
// attempt count, and no state, because the only state a room can observe is
// "you have this now".
type dispatchHandout struct {
	ID      string   `json:"id"`
	Token   string   `json:"token"`
	Harness string   `json:"harness"`
	Cwd     string   `json:"cwd,omitempty"`
	Title   string   `json:"title,omitempty"`
	Prompt  string   `json:"prompt,omitempty"`
	Why     string   `json:"why,omitempty"`
	Tags    []string `json:"tags,omitempty"`
	Window  string   `json:"window,omitempty"`
}

// handoutFor is what to send back to a room that just checked in.
//
// BEST EFFORT, LIKE EVERYTHING ELSE A ROOM TOUCHES. A store that cannot answer
// means this check-in carries no work, which is the same thing as an empty
// queue from the room's side. Failing the check-in instead would take the
// room's card report down with it, and the report is the part that was already
// working.
//
// A room that says it does not take launches is not handed any. The items stay
// queued and the board says why beside the room that is refusing them, which is
// a state somebody can act on. Handing them over to be bounced would spend the
// item's two attempts on a machine that was never going to run it.
func (d *Daemon) handoutFor(rep RoomReport) []dispatchHandout {
	// Any check-in is a chance to take back a claim that went nowhere. No
	// timer, because the room whose lease needs expiring is by definition the
	// one that is not checking in, and some other room is.
	if moved, err := d.st.ExpireDispatchClaims(store.DispatchLease); err != nil {
		log.Printf("[atrium] could not expire stale dispatch claims: %v", err)
	} else if moved > 0 {
		log.Printf("[atrium] took back %d dispatched item(s) whose room went quiet", moved)
	}

	// Both mean the same thing to the handout and different things to the
	// board: a standing no, and a room part way through the last batch. A room
	// that collected another batch mid-batch would run two of them end to end
	// and outlast the lease on the first.
	if !rep.Launches || rep.Busy {
		return nil
	}
	items, err := d.st.ClaimDispatches(rep.Name, store.MaxHandout)
	if err != nil {
		log.Printf("[atrium] could not hand work to room %q: %v", rep.Name, err)
		return nil
	}
	out := make([]dispatchHandout, 0, len(items))
	for _, it := range items {
		log.Printf("[atrium] handed %s to room %q: start %s%s",
			it.ID, rep.Name, it.Harness, cwdNote(it.Cwd))
		out = append(out, dispatchHandout{
			ID: it.ID, Token: it.Token, Harness: it.Harness, Cwd: it.Cwd,
			Title: it.Title, Prompt: it.Prompt, Why: it.Why,
			Tags: it.Tags, Window: it.Window,
		})
	}
	return out
}

func cwdNote(cwd string) string {
	if strings.TrimSpace(cwd) == "" {
		return " in whatever directory that runner is configured with"
	}
	return " in " + cwd
}

// Dispatches is the queue, for the board.
func (d *Daemon) Dispatches() any {
	items, err := d.st.Dispatches("")
	if err != nil {
		return map[string]any{"dispatches": []any{}, "error": err.Error()}
	}
	if items == nil {
		items = []*store.Dispatch{}
	}
	return map[string]any{"dispatches": items}
}

// QueueDispatch files a launch for another machine.
//
// NOTHING HERE IS CHECKED AGAINST THIS MACHINE. The harness id belongs to the
// room's harness table and the directory, if one is given at all, belongs to
// the room's filesystem. Validating either against what is configured here
// would refuse a correct dispatch because cdaws has a runner this desktop does
// not, which is the normal case rather than the exception.
//
// The room name is not checked against the rooms that have checked in either.
// A machine that is switched off right now is a machine you can still queue
// work for, and it collects the queue when it comes back. That is most of the
// point.
func (d *Daemon) QueueDispatch(body []byte) (any, error) {
	var req struct {
		Room    string   `json:"room"`
		Harness string   `json:"harness"`
		Cwd     string   `json:"cwd"`
		Title   string   `json:"title"`
		Prompt  string   `json:"prompt"`
		Why     string   `json:"why"`
		Tags    []string `json:"tags"`
		Window  string   `json:"window"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, err
	}
	item, err := d.st.QueueDispatch(store.Dispatch{
		Room: req.Room, Harness: req.Harness, Cwd: req.Cwd, Title: req.Title,
		Prompt: req.Prompt, Why: req.Why, Tags: req.Tags, Window: req.Window,
	})
	if err != nil {
		return nil, err
	}
	log.Printf("[atrium] queued %s for room %q: start %s%s",
		item.ID, item.Room, item.Harness, cwdNote(item.Cwd))
	d.ap.Broadcast("dispatch", item)
	return item, nil
}

// CancelDispatch withdraws an item nobody has taken yet.
func (d *Daemon) CancelDispatch(id string) error {
	if err := d.st.CancelDispatch(id); err != nil {
		return err
	}
	log.Printf("[atrium] withdrew dispatched item %s", id)
	if item, err := d.st.Dispatch(id); err == nil {
		d.ap.Broadcast("dispatch", item)
	}
	return nil
}

// handleDispatchResult is a room saying what it did with an item.
//
// ON THE HUMAN LISTENER, beside the check-in it answers, and for the same
// reason given in `rooms.go`: a room is another operator surface rather than a
// runner, and the agent listener closing on a halt must not take it with it.
//
// The token in the body is the whole of the authorization, and the store checks
// it inside the same statement that moves the row. There is no room identity
// here to trust and none is invented: whoever holds the token is whoever was
// handed the item, which is the only claim being made.
func (d *Daemon) handleDispatchResult(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	body, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	var res struct {
		Token   string `json:"token"`
		OK      bool   `json:"ok"`
		CardID  string `json:"card_id"`
		CardURL string `json:"card_url"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	state := store.DispatchFailed
	if res.OK {
		state = store.DispatchRunning
	}
	item, err := d.st.SettleDispatch(id, res.Token, state, res.CardID, res.CardURL, res.Error)
	if err != nil {
		// 409 rather than 400. The request was well formed and the item has
		// moved on, which is a thing the room should say once in its log and
		// then stop retrying.
		writeJSONErr(w, http.StatusConflict, err)
		return
	}
	if res.OK {
		log.Printf("[atrium] room %q started %s and filed it as %s",
			item.Room, item.ID, orDash(item.CardID))
	} else {
		log.Printf("[atrium] room %q refused %s: %s", item.Room, item.ID, item.Error)
	}
	d.ap.Broadcast("dispatch", item)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "-"
	}
	return s
}
