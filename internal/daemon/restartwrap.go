package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The restart wrap-up: before a room restart, each working card is asked to finish its step, write its state down
// and say it is ready. See docs/changes/r-graceful-room-restart.md.
//
// NOT A CONTEXT CYCLE. It borrows the cycle's prompt style, its typing gate (`typeLabelledGuarded`) and its ack
// (`atrium ready`, ready.go), and clears nothing: the room reopens the session with its conversation, so the state
// file is a help and not a requirement.
//
// ONLY A WORKING CARD IS ASKED. A card at its prompt, waiting on a dialog or a human, or finished, is already in
// the one state that survives a restart. Typing into it would start a turn and spend tokens for nothing.
//
// EVERY CARD ASKED GETS A RESTART WAKE, whether it answered, went idle or never answered, unless it already has
// one of its own. The wait is bounded, and a card that never answered is named in the log and on its history.

// wrapBy is the `by` on the events and wakes the wrap-up writes.
const wrapBy = "restart-wrap"

// wrapLabel goes ahead of the prompt, so nobody reads it as the operator's own words.
var wrapLabel = atriumLabel("restart:")

// wrapTiming is how often a card is looked at. A variable for a test.
var wrapTiming = struct{ poll time.Duration }{poll: 250 * time.Millisecond}

// The outcomes of one card.
const (
	wrapReady      = "ready"
	wrapIdle       = "idle"
	wrapUnanswered = "unanswered"
	// wrapSkipped is a card that was not prompted: it settled before the prompt was typed, or its runner went.
	wrapSkipped = "skipped"
)

func restartWrapPrompt(path, bin string) string {
	return "the room is restarting. wrap up the current step, write your state to " + path +
		", then run " + bin + " ready."
}

func restartWrapWake(path string, wrote bool, outcome string) string {
	s := "the room restarted"
	if outcome == wrapUnanswered {
		s += " before you answered its wrap-up"
	}
	s += ". carry on where you left off, and check git status first."
	if wrote {
		s += " your state is in " + path + "."
	}
	return s
}

// restartWrap is the wrap-up under way: one at a time, with the cards waiting for their ack.
type restartWrap struct {
	mu      sync.Mutex
	running bool
	cancel  context.CancelFunc
	acks    map[string]chan struct{}
}

func newRestartWrap() *restartWrap { return &restartWrap{acks: map[string]chan struct{}{}} }

// waiting is whether a wrap-up waits on this card's ack.
func (w *restartWrap) waiting(taskID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	_, ok := w.acks[taskID]
	return ok
}

// ack records `atrium ready` from a card and says whether one was waited for.
func (w *restartWrap) ack(taskID string) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch, ok := w.acks[taskID]
	if !ok {
		return false
	}
	delete(w.acks, taskID)
	close(ch)
	return true
}

func (w *restartWrap) expect(taskID string) chan struct{} {
	w.mu.Lock()
	defer w.mu.Unlock()
	ch := make(chan struct{})
	w.acks[taskID] = ch
	return ch
}

func (w *restartWrap) forget(taskID string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	delete(w.acks, taskID)
}

// begin claims the wrap-up. False when one is already running.
func (w *restartWrap) begin(cancel context.CancelFunc) bool {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.running {
		return false
	}
	w.running, w.cancel = true, cancel
	return true
}

func (w *restartWrap) end() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.running, w.cancel = false, nil
}

// cancelRunning ends the wrap-up under way, if any: the restart is no longer waiting.
func (w *restartWrap) cancelRunning() {
	w.mu.Lock()
	cancel := w.cancel
	w.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// WrapReport is what a wrap-up did, cards named by title.
type WrapReport struct {
	Asked      []string `json:"asked,omitempty"`
	Ready      []string `json:"ready,omitempty"`
	Idle       []string `json:"idle,omitempty"`
	Unanswered []string `json:"unanswered,omitempty"`
	Woken      []string `json:"woken,omitempty"`
	WaitedMS   int64    `json:"waited_ms"`
	Cancelled  bool     `json:"cancelled,omitempty"`
	Already    bool     `json:"already,omitempty"`
}

// wrapCandidate is a card that is working and so is asked.
func (d *Daemon) wrapCandidate(t *store.Task) bool {
	if !d.cycleSubject(t) || t.Status != store.StatusRunning || d.nctx.get(t.ID) != nil {
		return false
	}
	return !d.act.dialogOpen(t.ID) && d.ncBusy(t.ID)
}

// wrapUpForRestart asks every working card to wrap up, waits for each to say ready or go idle (at most wait), queues
// a restart wake on each card asked and returns what happened. It narrates every step. The caller then restarts.
func (d *Daemon) wrapUpForRestart(ctx context.Context, wait time.Duration, why string) WrapReport {
	rep := WrapReport{}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	if !d.wrap.begin(cancel) {
		log.Printf("[atrium] restart wrap-up: one is already waiting, not starting another")
		rep.Already = true
		return rep
	}
	defer d.wrap.end()

	start := time.Now()
	var cards []*store.Task
	for _, r := range d.sup.all() {
		t, err := d.st.Get(r.taskID)
		if err != nil || t == nil {
			continue
		}
		if d.wrapCandidate(t) {
			cards = append(cards, t)
		}
	}
	if len(cards) == 0 {
		log.Printf("[atrium] restart wrap-up: no card is working, nothing to wait for")
		return rep
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
	log.Printf("[atrium] restart wrap-up (%s): asking %d working card(s) to wrap up, waiting up to %s",
		orWord(why, "no reason given"), len(cards), wait)

	tctx, tcancel := context.WithTimeout(ctx, wait)
	defer tcancel()
	type result struct {
		t       *store.Task
		outcome string
		path    string
	}
	out := make(chan result, len(cards))
	for _, t := range cards {
		path, err := d.prepareHandoff(t)
		if err != nil {
			log.Printf("[atrium] restart wrap-up: %v. %s is asked without a state file", err, t.DisplayTitle())
			path = d.handoffPath(t)
		}
		ack := d.wrap.expect(t.ID)
		go func(t *store.Task, path string) {
			out <- result{t: t, outcome: d.wrapCard(tctx, t, path, ack), path: path}
		}(t, path)
	}
	results := make([]result, 0, len(cards))
	for range cards {
		results = append(results, <-out)
	}
	rep.Cancelled = ctx.Err() != nil
	for _, r := range results {
		d.wrap.forget(r.t.ID)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].t.ID < results[j].t.ID })

	for _, r := range results {
		title := r.t.DisplayTitle()
		switch r.outcome {
		case wrapSkipped:
			log.Printf("[atrium] restart wrap-up: %s settled before it was asked, left alone", title)
			continue
		case wrapReady:
			rep.Ready = append(rep.Ready, title)
		case wrapIdle:
			rep.Idle = append(rep.Idle, title)
		default:
			rep.Unanswered = append(rep.Unanswered, title)
			if rep.Cancelled {
				log.Printf("[atrium] restart wrap-up: %s was cut off, the wait was ended", title)
			} else {
				log.Printf("[atrium] restart wrap-up: %s did not answer within %s", title, wait)
			}
			if err := d.st.AppendEvent(r.t.ID, store.EventNotified, map[string]any{
				"by": wrapBy, "unanswered": true,
				"text": "restart: did not answer the wrap-up in " + wait.String(),
			}); err != nil {
				log.Printf("[atrium] could not record the unanswered wrap-up on %s: %v", r.t.ID, err)
			}
		}
		rep.Asked = append(rep.Asked, title)
		if r.outcome != wrapUnanswered {
			log.Printf("[atrium] restart wrap-up: %s is %s", title, r.outcome)
		}
		if d.wake.get(r.t.ID) != nil {
			log.Printf("[atrium] restart wrap-up: %s already has a wake of its own, keeping it", title)
			continue
		}
		_, werr := os.Stat(r.path)
		text := restartWrapWake(r.path, werr == nil, r.outcome)
		if _, _, err := d.queueWake(r.t.ID, text, wrapBy); err != nil {
			log.Printf("[atrium] restart wrap-up: could not queue a wake on %s: %v", title, err)
			continue
		}
		rep.Woken = append(rep.Woken, title)
	}
	rep.WaitedMS = time.Since(start).Milliseconds()
	if len(rep.Unanswered) > 0 {
		log.Printf("[atrium] restart wrap-up: restarting with %d card(s) that never answered: %s",
			len(rep.Unanswered), strings.Join(rep.Unanswered, ", "))
	}
	log.Printf("[atrium] restart wrap-up done in %s: %d asked, %d ready, %d idle, %d unanswered, %d wake(s) queued",
		time.Since(start).Round(time.Millisecond), len(rep.Asked), len(rep.Ready), len(rep.Idle),
		len(rep.Unanswered), len(rep.Woken))
	return rep
}

// wrapCard types the prompt into one card and waits for its ack or for it to go idle.
func (d *Daemon) wrapCard(ctx context.Context, t *store.Task, path string, ack <-chan struct{}) string {
	id := t.ID
	mid := d.midTurnInputFor(id)
	typed, typedBusy, turn0 := false, false, 0
	tick := time.NewTicker(wrapTiming.poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if !typed {
				return wrapSkipped
			}
			return wrapUnanswered
		case <-ack:
			return wrapReady
		case <-tick.C:
		}
		run := d.sup.get(id)
		if run == nil {
			return wrapSkipped
		}
		if !typed {
			if !d.ncBusy(id) && d.act.sinceBusy(id) >= ncTiming.turnSettle {
				return wrapSkipped
			}
			busy, turn := false, 0
			ok := func() bool {
				if d.act.dialogOpen(id) {
					return false
				}
				busy = d.ncBusy(id)
				turn = d.act.turnsBegun(id)
				return mid || !busy
			}
			text := restartWrapPrompt(path, atriumBinary())
			wrote, err := d.typeLabelledGuarded(run, id, wrapLabel, text, ok)
			if err != nil || !wrote {
				continue
			}
			typed, typedBusy, turn0 = true, busy, turn
			log.Printf("[atrium] restart wrap-up: asked %s to wrap up", t.DisplayTitle())
			if err := d.st.AppendEvent(id, store.EventPrompted, map[string]any{
				"text": text, "via": "terminal", "from": wrapBy,
			}); err != nil {
				log.Printf("[atrium] could not record the wrap-up prompt on %s: %v", id, err)
			}
			continue
		}
		// Asked. Idle is a turn that is over and stayed over. A prompt typed between turns must have started one.
		if !d.ncBusy(id) && d.act.sinceBusy(id) >= ncTiming.turnSettle &&
			(typedBusy || d.act.turnsBegun(id) > turn0) {
			return wrapIdle
		}
	}
}

// handleRestartWrap is POST /v1/restart-wrap: the wrap-up alone, answered when it is over. The hub-asked restart
// calls it before it schedules the restarter. `?wait=` is seconds, else the room's setting.
func (d *Daemon) handleRestartWrap(w http.ResponseWriter, r *http.Request) {
	if !d.shutdownAllowed(w, r) {
		return
	}
	wait := d.st.RestartWrapWait()
	if n := parseSeconds(r.URL.Query().Get("wait")); n > 0 {
		wait = n
	}
	rep := d.wrapUpForRestart(r.Context(), wait, r.URL.Query().Get("why"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

func parseSeconds(v string) time.Duration {
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
