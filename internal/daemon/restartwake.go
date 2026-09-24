package daemon

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The after-restart wake: a prompt typed into a card's terminal once, when its
// runner is back after a restart. See docs/restart-wake.md.
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
// THE ROW IS THE TRUTH. The daemon keeps a mirror so the board can draw it
// without a query per card, and the mirror is only ever written after the store.

const (
	// restartWakeTTL is how long a wake waits for its card to come back. A
	// restart is seconds, and a cold machine resuming ten sessions is minutes.
	// Past this the restart it was queued for is not happening, and a wake typed
	// hours later would be a prompt out of nowhere.
	restartWakeTTL = 30 * time.Minute

	// wakeTickEvery is how often waiting wakes are looked at. The same front
	// step as the peer backoff, so a wake lands about as soon as the gate opens.
	wakeTickEvery = 2 * time.Second

	// wakeSettle is how long after the runner's SessionStart hook a wake waits.
	// The hook fires as the session starts, which is a moment before its input
	// box is drawn, and bytes typed before then are lost.
	wakeSettle = 5 * time.Second

	// wakeNoHook is how long a runner that never posts SessionStart has to be up
	// before a wake is typed into it anyway. A runner with no hooks has no other
	// signal that it is ready.
	wakeNoHook = time.Minute

	// wakeExpiredKept is how long an expired wake stays on its card. Long enough
	// to be seen the next morning, then it ages off. The timeline keeps the
	// record either way.
	wakeExpiredKept = 24 * time.Hour
)

// wakes mirrors the restart_wake table and remembers when each card's session
// last started in this daemon.
type wakes struct {
	mu sync.Mutex
	by map[string]*store.RestartWake
	// sessionAt is the last SessionStart hook per card, in this process. In
	// memory on purpose: the question is whether the runner that is up NOW has
	// started, and no earlier process can answer that.
	sessionAt map[string]time.Time

	// deliver serializes a delivery attempt against a queue or a clear on the
	// same wake, so a wake replaced mid-attempt is never typed as well as the
	// wake that replaced it. Separate from mu so the board is never held behind
	// a paste.
	deliver sync.Mutex
}

func newWakes() *wakes {
	return &wakes{by: map[string]*store.RestartWake{}, sessionAt: map[string]time.Time{}}
}

func (wk *wakes) put(w *store.RestartWake) {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	cp := *w
	wk.by[w.TaskID] = &cp
}

func (wk *wakes) forget(taskID string) {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	delete(wk.by, taskID)
}

func (wk *wakes) get(taskID string) *store.RestartWake {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	w := wk.by[taskID]
	if w == nil {
		return nil
	}
	cp := *w
	return &cp
}

func (wk *wakes) all() []*store.RestartWake {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	out := make([]*store.RestartWake, 0, len(wk.by))
	for _, w := range wk.by {
		cp := *w
		out = append(out, &cp)
	}
	return out
}

func (wk *wakes) sawSession(taskID string, at time.Time) {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	wk.sessionAt[taskID] = at
}

func (wk *wakes) sessionStarted(taskID string) (time.Time, bool) {
	wk.mu.Lock()
	defer wk.mu.Unlock()
	at, ok := wk.sessionAt[taskID]
	return at, ok
}

// loadWakes fills the mirror from the store at startup.
func (d *Daemon) loadWakes() {
	ws, err := d.st.RestartWakes()
	if err != nil {
		log.Printf("[atrium] could not read the after-restart wakes: %v", err)
		return
	}
	for _, w := range ws {
		d.wake.put(w)
	}
	if n := len(ws); n > 0 {
		log.Printf("[atrium] %d after-restart wake(s) waiting for their cards", n)
	}
}

// wakeSawSession records a SessionStart for a card. Called from onSession.
func (d *Daemon) wakeSawSession(taskID string) {
	if d.wake != nil {
		d.wake.sawSession(taskID, time.Now())
	}
}

// wakeFor is the card's wake as the board draws it, or nil.
func (d *Daemon) wakeFor(taskID string) any {
	w := d.wake.get(taskID)
	if w == nil {
		return nil
	}
	return wakeView(w)
}

func wakeView(w *store.RestartWake) map[string]any {
	v := map[string]any{
		"text": w.Text, "by": w.By, "queued_at": w.QueuedAt, "expires_at": w.ExpiresAt,
		"state": "waiting",
	}
	if w.ExpiredAt != nil {
		v["state"] = "expired"
		v["expired_at"] = *w.ExpiredAt
	}
	return v
}

// queueWake stores a wake for a card and mirrors it.
func (d *Daemon) queueWake(taskID, text, by string) (*store.RestartWake, *store.RestartWake, error) {
	d.wake.deliver.Lock()
	defer d.wake.deliver.Unlock()
	w, old, err := d.st.SetRestartWake(taskID, text, by, restartWakeTTL)
	if err != nil {
		return nil, nil, err
	}
	d.wake.put(w)
	log.Printf("[atrium] after-restart wake queued for %s by %s", taskID, orWord(w.By, "an unnamed caller"))
	d.publishTask(taskID)
	return w, old, nil
}

// clearWake removes a card's wake, waiting or expired.
func (d *Daemon) clearWake(taskID, by string) (bool, error) {
	d.wake.deliver.Lock()
	defer d.wake.deliver.Unlock()
	gone, err := d.st.ClearRestartWake(taskID, by, false)
	if err != nil {
		return false, err
	}
	d.wake.forget(taskID)
	d.publishTask(taskID)
	return gone, nil
}

// wakeLoop looks at the waiting wakes until ctx ends.
func (d *Daemon) wakeLoop(ctx context.Context) {
	t := time.NewTicker(wakeTickEvery)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			d.wakeTick(now)
		}
	}
}

// wakeTick is one look at every wake: age off, expire, or try to type it in.
func (d *Daemon) wakeTick(now time.Time) {
	for _, w := range d.wake.all() {
		d.tryWake(w, now)
	}
}

// tryWake handles one wake: age an expired one off, expire one past its bound,
// or type it in when the runner is back and the gate is open.
func (d *Daemon) tryWake(w *store.RestartWake, now time.Time) {
	d.wake.deliver.Lock()
	defer d.wake.deliver.Unlock()
	// Re-read under the delivery lock: a queue or a clear may have landed since
	// the snapshot, and that newer state is the one to act on.
	cur := d.wake.get(w.TaskID)
	if cur == nil || !cur.QueuedAt.Equal(w.QueuedAt) {
		return
	}
	w = cur

	if w.ExpiredAt != nil {
		if now.Sub(*w.ExpiredAt) >= wakeExpiredKept {
			if _, err := d.st.ClearRestartWake(w.TaskID, "", true); err != nil {
				log.Printf("[atrium] could not age off the expired wake on %s: %v", w.TaskID, err)
				return
			}
			d.wake.forget(w.TaskID)
			d.publishTask(w.TaskID)
		}
		return
	}
	if !now.Before(w.ExpiresAt) {
		at := now.UTC().Truncate(time.Millisecond)
		ok, err := d.st.ExpireRestartWake(w.TaskID, w.QueuedAt, at)
		if err != nil {
			log.Printf("[atrium] could not expire the wake on %s: %v", w.TaskID, err)
			return
		}
		if ok {
			w.ExpiredAt = &at
			d.wake.put(w)
			log.Printf("[atrium] after-restart wake for %s expired: its runner did not come back within %s",
				w.TaskID, restartWakeTTL)
			d.publishTask(w.TaskID)
		}
		return
	}

	run := d.sup.get(w.TaskID)
	if run == nil || !d.wakeRunnerReady(w, run, now) {
		return
	}
	// The same gate every automated write goes through, plus the turn. A dialog
	// on screen would be answered by the Enter. A runner mid-turn holds what is
	// typed and merges it with the operator's draft. See docs/typing-race.md.
	if d.act.dialogOpen(w.TaskID) || d.act.midTurn(w.TaskID) {
		return
	}
	// Empty line and a quiet keyboard, re-checked under the input lock. A closed
	// gate writes nothing, and the next tick asks again.
	wrote, err := d.typeThroughGate(run, w.TaskID, "", w.Text)
	if err != nil {
		log.Printf("[atrium] could not type the wake into %s: %v", w.TaskID, err)
		return
	}
	if !wrote {
		return
	}
	if _, err := d.st.TakeRestartWake(w.TaskID, w.QueuedAt); err != nil {
		// Typed, and the row could not go. Dropped from the mirror anyway, so it
		// is not typed a second time in this process.
		log.Printf("[atrium] typed the wake into %s but could not delete it: %v", w.TaskID, err)
	}
	d.wake.forget(w.TaskID)
	log.Printf("[atrium] after-restart wake typed into %s", w.TaskID)
	d.publishTask(w.TaskID)
}

// wakeRunnerReady reports whether the runner up now is the one the wake is for,
// and is ready to take input.
//
// THE RUNNER MUST HAVE STARTED AFTER THE WAKE WAS QUEUED. The wake is queued
// while the old runner is still alive, and typing into it would be the forced
// turn this is not.
func (d *Daemon) wakeRunnerReady(w *store.RestartWake, run *runner, now time.Time) bool {
	if !run.started.After(w.QueuedAt) {
		return false
	}
	if at, ok := d.wake.sessionStarted(w.TaskID); ok && !at.Before(run.started) {
		return now.Sub(at) >= wakeSettle
	}
	return now.Sub(run.started) >= wakeNoHook
}

// handleRestartWake is `/v1/tasks/{id}/restart-wake`: POST queues, GET reads,
// DELETE clears. `{id}` is a card id or a wire name, so a deploy script can name
// the session it knows.
func (d *Daemon) handleRestartWake(w http.ResponseWriter, r *http.Request) {
	task, err := d.st.Get(r.PathValue("id"))
	if err != nil {
		task, err = d.st.GetByWireName(d.st.Qualify(r.PathValue("id")))
	}
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, errString("no card called "+r.PathValue("id")))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	switch r.Method {
	case http.MethodGet:
		out := map[string]any{"card": task.ID, "wake": nil}
		if cur := d.wake.get(task.ID); cur != nil {
			out["wake"] = wakeView(cur)
		}
		_ = json.NewEncoder(w).Encode(out)
	case http.MethodDelete:
		gone, err := d.clearWake(task.ID, r.URL.Query().Get("by"))
		if err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"card": task.ID, "cleared": gone})
	case http.MethodPost:
		var in struct {
			Text string `json:"text"`
			By   string `json:"by"`
		}
		if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
			writeJSONErr(w, http.StatusBadRequest, err)
			return
		}
		by := strings.TrimSpace(in.By)
		if by == "" {
			by = "api"
		}
		nw, old, err := d.queueWake(task.ID, in.Text, by)
		switch {
		case errors.Is(err, store.ErrWakeText):
			writeJSONErr(w, http.StatusBadRequest, err)
			return
		case errors.Is(err, sql.ErrNoRows):
			writeJSONErr(w, http.StatusNotFound, errString("no card called "+r.PathValue("id")))
			return
		case err != nil:
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		out := map[string]any{
			"card": task.ID, "queued": true, "wake": wakeView(nw),
			"note": "typed into this card's terminal once, after its runner comes back from a restart, " +
				"when the line is empty and the turn is over. it expires unsent after " +
				restartWakeTTL.String() + ".",
		}
		if old != nil {
			out["replaced"] = old.Text
		}
		_ = json.NewEncoder(w).Encode(out)
	default:
		writeJSONErr(w, http.StatusMethodNotAllowed, errString("GET, POST or DELETE"))
	}
}
