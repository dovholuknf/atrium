package cycle

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// The after-restart wake: a prompt typed into a card's terminal once, when its
// runner is back after a restart. See docs/runtime/restart-wake.md.
//
// A room restart ends every terminal it owns. The fixtures and reopenSaved bring
// the sessions back, and a resumed session sits idle until somebody types into
// it. The orchestrator is one of those sessions, so a deploy it ran left it
// waiting for clint to type "we up". A wake is that line, queued before the
// restart and typed by the room after.
//
// NOT A FORCED TURN. `peers.go` explains why one session's words are gated on
// another's terminal. A wake is queued by the session for itself, through
// `atrium_wake_after_restart`, or by a script a human runs, and it only resumes
// work a restart interrupted. It still goes through the same gate as every
// automated write: an empty line, a quiet keyboard, and a turn that has ended.
//
// NO EXPIRY. A wake waits until its card's runner is back, however long that
// takes. It goes when it is typed, cleared or replaced, or with its card.
//
// LABELLED. It is typed behind a grey `[atrium] restart wake:` label, the same
// style as a peer's banner, so nobody reads it as the operator's words.
//
// THE ROW IS THE TRUTH. The daemon keeps a mirror so the board can draw it
// without a query per card, and the mirror is only ever written after the store.

// wakeLabel goes ahead of a wake's text. See atriumLabel.
var WakeLabel = shared.AtriumLabel("restart wake:")

const (
	// wakeTickEvery is how often waiting wakes are looked at. The same front
	// step as the peer backoff, so a wake lands about as soon as the gate opens.
	WakeTickEvery = 2 * time.Second

	// wakeSettle is how long after the runner's SessionStart hook a wake waits.
	// The hook fires as the session starts, which is a moment before its input
	// box is drawn, and bytes typed before then are lost.
	WakeSettle = 5 * time.Second

	// wakeNoHook is how long a runner that never posts SessionStart has to be up
	// before a wake is typed into it anyway. A runner with no hooks has no other
	// signal that it is ready.
	WakeNoHook = time.Minute
)

// wakes mirrors the restart_wake table and remembers when each card's session
// last started in this daemon.
type Wakes struct {
	Mu sync.Mutex
	By map[string]*store.RestartWake
	// sessionAt is the last SessionStart hook per card, in this process. In
	// memory on purpose: the question is whether the runner that is up NOW has
	// started, and no earlier process can answer that.
	SessionAt map[string]time.Time
	// convAt is the conversation id that SessionStart carried. In memory, like it.
	ConvAt map[string]string

	// deliver serializes a delivery attempt against a queue or a clear on the
	// same wake, so a wake replaced mid-attempt is never typed as well as the
	// wake that replaced it. Separate from mu so the board is never held behind
	// a paste.
	Deliver sync.Mutex
}

func NewWakes() *Wakes {
	return &Wakes{By: map[string]*store.RestartWake{}, SessionAt: map[string]time.Time{}, ConvAt: map[string]string{}}
}

func (wk *Wakes) Put(w *store.RestartWake) {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	cp := *w
	wk.By[w.TaskID] = &cp
}

func (wk *Wakes) Forget(taskID string) {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	delete(wk.By, taskID)
}

func (wk *Wakes) Get(taskID string) *store.RestartWake {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	w := wk.By[taskID]
	if w == nil {
		return nil
	}
	cp := *w
	return &cp
}

func (wk *Wakes) All() []*store.RestartWake {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	out := make([]*store.RestartWake, 0, len(wk.By))
	for _, w := range wk.By {
		cp := *w
		out = append(out, &cp)
	}
	return out
}

func (wk *Wakes) SawSession(taskID string, at time.Time) {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	wk.SessionAt[taskID] = at
}

// sawConversation keeps the conversation id of the card's latest SessionStart. An
// empty id is not recorded: a hook that could not say leaves the last one.
func (wk *Wakes) SawConversation(taskID, conv string) {
	if conv == "" {
		return
	}
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	wk.ConvAt[taskID] = conv
}

// conversation is the id the card's latest SessionStart reported, or "".
func (wk *Wakes) Conversation(taskID string) string {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	return wk.ConvAt[taskID]
}

func (wk *Wakes) SessionStarted(taskID string) (time.Time, bool) {
	wk.Mu.Lock()
	defer wk.Mu.Unlock()
	at, ok := wk.SessionAt[taskID]
	return at, ok
}

// loadWakes fills the mirror from the store at startup.
func (d *Service) LoadWakes() {
	ws, err := d.St.RestartWakes()
	if err != nil {
		log.Printf("[atrium] could not read the after-restart wakes: %v", err)
		return
	}
	for _, w := range ws {
		d.Wake.Put(w)
	}
	if n := len(ws); n > 0 {
		log.Printf("[atrium] %d after-restart wake(s) waiting for their cards", n)
	}
}

// wakeSawSession records a SessionStart for a card. Called from onSession.
//
// `conv` is the conversation id the hook reported. A new one is the proof a failed
// new-context chip waits for: see `newContexts.sessionStarted`.
func (d *Service) WakeSawSession(taskID, conv string) {
	if d.Wake != nil {
		d.Wake.SawSession(taskID, time.Now())
		d.Wake.SawConversation(taskID, conv)
	}
	if d.Nctx.SessionStarted(taskID, conv) {
		d.Publish(taskID)
	}
}

// wakeFor is the card's wake as the board draws it, or nil.
func (d *Service) WakeFor(taskID string) any {
	w := d.Wake.Get(taskID)
	if w == nil {
		return nil
	}
	return WakeView(w)
}

func WakeView(w *store.RestartWake) map[string]any {
	return map[string]any{"text": w.Text, "by": w.By, "queued_at": w.QueuedAt, "state": "waiting"}
}

// queueWake stores a wake for a card and mirrors it.
func (d *Service) QueueWake(taskID, text, by string) (*store.RestartWake, *store.RestartWake, error) {
	d.Wake.Deliver.Lock()
	defer d.Wake.Deliver.Unlock()
	w, old, err := d.St.SetRestartWake(taskID, text, by)
	if err != nil {
		return nil, nil, err
	}
	d.Wake.Put(w)
	log.Printf("[atrium] after-restart wake queued for %s by %s", taskID, shared.OrWord(w.By, "an unnamed caller"))
	d.Publish(taskID)
	return w, old, nil
}

// clearWake removes a card's wake.
func (d *Service) ClearWake(taskID, by string) (bool, error) {
	d.Wake.Deliver.Lock()
	defer d.Wake.Deliver.Unlock()
	gone, err := d.St.ClearRestartWake(taskID, by)
	if err != nil {
		return false, err
	}
	d.Wake.Forget(taskID)
	d.Publish(taskID)
	return gone, nil
}

// wakeLoop looks at the waiting wakes until ctx ends.
func (d *Service) WakeLoop(ctx context.Context) {
	t := time.NewTicker(WakeTickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			d.WakeTick(now)
		}
	}
}

// wakeTick is one look at every wake, typing in each one whose card is ready.
func (d *Service) WakeTick(now time.Time) {
	for _, w := range d.Wake.All() {
		d.TryWake(w, now)
	}
}

// tryWake types one wake in when the runner is back and the gate is open, and
// otherwise leaves it for the next tick.
func (d *Service) TryWake(w *store.RestartWake, now time.Time) {
	d.Wake.Deliver.Lock()
	defer d.Wake.Deliver.Unlock()
	// Re-read under the delivery lock: a queue or a clear may have landed since
	// the snapshot, and that newer state is the one to act on.
	cur := d.Wake.Get(w.TaskID)
	if cur == nil || !cur.QueuedAt.Equal(w.QueuedAt) {
		return
	}
	w = cur

	// A notice waiting when the setting goes off is dropped, not held for later.
	if w.By == store.UnexpectedExitBy && !d.St.UnexpectedExitOn() {
		if _, err := d.St.ClearRestartWake(w.TaskID, "the unexpected-exit setting"); err != nil {
			log.Printf("[atrium] could not drop the unexpected-exit notice on %s: %v", w.TaskID, err)
			return
		}
		d.Wake.Forget(w.TaskID)
		d.Publish(w.TaskID)
		return
	}

	// A card atrium does not supervise has no runner here, and its wake waits.
	run := d.Sup.Get(w.TaskID)
	if run == nil || !d.WakeRunnerReady(w, run, now) {
		return
	}
	// The same gate every automated write goes through, plus the turn. A dialog
	// on screen would be answered by the Enter. A runner mid-turn holds what is
	// typed and merges it with the operator's draft. See docs/terminal/typing-race.md.
	if d.Act.DialogOpen(w.TaskID) || d.Act.MidTurn(w.TaskID) {
		return
	}
	// Empty line and a quiet keyboard, re-checked under the input lock. A closed
	// gate writes nothing, and the next tick asks again. The label marks it as
	// atrium's, and keeps the prompt it starts from marking the turn seen.
	wrote, err := d.TypeLabelledThroughGate(run, w.TaskID, d.LabelFor(w), w.Text)
	if err != nil {
		log.Printf("[atrium] could not type the wake into %s: %v", w.TaskID, err)
		return
	}
	if !wrote {
		return
	}
	if _, err := d.St.TakeRestartWake(w.TaskID, w.QueuedAt); err != nil {
		// Typed, and the row could not go. Dropped from the mirror anyway, so it
		// is not typed a second time in this process.
		log.Printf("[atrium] typed the wake into %s but could not delete it: %v", w.TaskID, err)
	}
	d.Wake.Forget(w.TaskID)
	log.Printf("[atrium] after-restart wake (%s) typed into %s", shared.OrWord(w.By, "unnamed"), w.TaskID)
	d.Publish(w.TaskID)
}

// wakeRunnerReady reports whether the runner up now is the one the wake is for,
// and is ready to take input.
//
// THE RUNNER MUST HAVE STARTED AFTER THE WAKE WAS QUEUED. The wake is queued
// while the old runner is still alive, and typing into it would be the forced
// turn this is not.
func (d *Service) WakeRunnerReady(w *store.RestartWake, run shared.Runner, now time.Time) bool {
	// A runner picked up from the pty host after a restart is the runner the wake was queued for, since the restart
	// did not end it. It counts from when this daemon took it over.
	started := run.Started()
	if !run.ReattachedAt().IsZero() {
		started = run.ReattachedAt()
	}
	if !started.After(w.QueuedAt) && run.ReattachedAt().IsZero() {
		return false
	}
	if at, ok := d.Wake.SessionStarted(w.TaskID); ok && !at.Before(started) {
		return now.Sub(at) >= WakeSettle
	}
	return now.Sub(started) >= WakeNoHook
}

// handleRestartWake is `/v1/tasks/{id}/restart-wake`: POST queues, GET reads,
// DELETE clears. `{id}` is a card id or a wire name, so a deploy script can name
// the session it knows.
func (d *Service) HandleRestartWake(w http.ResponseWriter, r *http.Request) {
	task, err := d.St.Get(r.PathValue("id"))
	if err != nil {
		task, err = d.St.GetByWireName(d.St.Qualify(r.PathValue("id")))
	}
	if err != nil {
		small.WriteJSONErr(w, http.StatusNotFound, small.ErrString("no card called "+r.PathValue("id")))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		out := map[string]any{"card": task.ID, "wake": nil}
		if cur := d.Wake.Get(task.ID); cur != nil {
			out["wake"] = WakeView(cur)
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodDelete:
		gone, err := d.ClearWake(task.ID, r.URL.Query().Get("by"))
		if err != nil {
			small.WriteJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"card": task.ID, "cleared": gone})
	case http.MethodPost:
		var in struct {
			Text string `json:"text"`
			By   string `json:"by"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
			small.WriteJSONErr(w, http.StatusBadRequest, err)
			return
		}
		by := strings.TrimSpace(in.By)
		if by == "" {
			by = "api"
		}
		nw, old, err := d.QueueWake(task.ID, in.Text, by)
		switch {
		case errors.Is(err, store.ErrWakeText):
			small.WriteJSONErr(w, http.StatusBadRequest, err)
			return
		case errors.Is(err, sql.ErrNoRows):
			small.WriteJSONErr(w, http.StatusNotFound, small.ErrString("no card called "+r.PathValue("id")))
			return
		case err != nil:
			small.WriteJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		out := map[string]any{
			"card": task.ID, "queued": true, "wake": WakeView(nw),
			"note": "typed into this card's terminal once, after its runner comes back from a restart, " +
				"when the line is empty and the turn is over. it waits however long that takes.",
		}
		if old != nil {
			out["replaced"] = old.Text
		}
		_ = json.NewEncoder(w).Encode(out)
	default:
		small.WriteJSONErr(w, http.StatusMethodNotAllowed, small.ErrString("GET, POST or DELETE"))
	}
}
