package cycle

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
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
const WrapBy = "restart-wrap"

// wrapLabel goes ahead of the prompt, so nobody reads it as the operator's own words.
var WrapLabel = shared.AtriumLabel("restart:")

// wrapTimings is how often a card is looked at.
type WrapTimings struct{ Poll time.Duration }

// The outcomes of one card.
const (
	WrapReady      = "ready"
	WrapIdle       = "idle"
	WrapUnanswered = "unanswered"
	// wrapSkipped is a card that was not prompted: it settled before the prompt was typed, or its runner went.
	WrapSkipped = "skipped"
)

func RestartWrapPrompt(path, bin string) string {
	return "the room is restarting. wrap up the current step, write your state to " + path +
		", then run " + bin + " ready."
}

func RestartWrapWake(path string, wrote bool, outcome string) string {
	s := "the room restarted"
	if outcome == WrapUnanswered {
		s += " before you answered its wrap-up"
	}
	s += ". carry on where you left off, and check git status first."
	if wrote {
		s += " your state is in " + path + "."
	}
	return s
}

// restartWrap is the wrap-up under way: one at a time, with the cards waiting for their ack.
type RestartWrap struct {
	Mu      sync.Mutex
	Running bool
	Cancel  context.CancelFunc
	Cur     *WrapRun
	Acks    map[string]chan struct{}
}

// wrapRun is the wrap-up under way, for a second stop to join. rep is written before done is closed.
type WrapRun struct {
	Done     chan struct{}
	Deadline time.Time
	Rep      WrapReport
}

// wrapJoinMargin is how long past the running wrap-up's own deadline a joiner waits before it gives up on it. A
// variable for a test.
var WrapJoinMargin = 5 * time.Second

func NewRestartWrap() *RestartWrap { return &RestartWrap{Acks: map[string]chan struct{}{}} }

// waiting is whether a wrap-up waits on this card's ack.
func (w *RestartWrap) Waiting(taskID string) bool {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	_, ok := w.Acks[taskID]
	return ok
}

// ack records `atrium ready` from a card and says whether one was waited for.
func (w *RestartWrap) Ack(taskID string) bool {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	ch, ok := w.Acks[taskID]
	if !ok {
		return false
	}
	delete(w.Acks, taskID)
	close(ch)
	return true
}

func (w *RestartWrap) Expect(taskID string) chan struct{} {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	ch := make(chan struct{})
	w.Acks[taskID] = ch
	return ch
}

func (w *RestartWrap) Forget(taskID string) {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	delete(w.Acks, taskID)
}

// begin claims the wrap-up, due by deadline. When one is already running it returns that one to join and false.
func (w *RestartWrap) Begin(cancel context.CancelFunc, deadline time.Time) (*WrapRun, bool) {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	if w.Running {
		return w.Cur, false
	}
	w.Running, w.Cancel = true, cancel
	w.Cur = &WrapRun{Done: make(chan struct{}), Deadline: deadline}
	return w.Cur, true
}

// end releases the wrap-up with what it did, and wakes whoever joined it.
func (w *RestartWrap) End(rep WrapReport) {
	w.Mu.Lock()
	defer w.Mu.Unlock()
	run := w.Cur
	w.Running, w.Cancel, w.Cur = false, nil, nil
	if run != nil {
		run.Rep = rep
		close(run.Done)
	}
}

// cancelRunning ends the wrap-up under way, if any: the restart is no longer waiting.
func (w *RestartWrap) CancelRunning() {
	w.Mu.Lock()
	cancel := w.Cancel
	w.Mu.Unlock()
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
func (d *Service) WrapCandidate(t *store.Task) bool {
	if !d.CycleSubject(t) || t.Status != store.StatusRunning || d.Nctx.Get(t.ID) != nil {
		return false
	}
	return !d.Act.DialogOpen(t.ID) && d.NcBusy(t.ID)
}

// wrapUpForRestart asks every working card to wrap up, waits for each to say ready or go idle (at most wait), queues
// a restart wake on each card asked and returns what happened. It narrates every step. The caller then restarts.
func (d *Service) WrapUpForRestart(ctx context.Context, wait time.Duration, why string) (rep WrapReport) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	run, mine := d.Wrap.Begin(cancel, time.Now().Add(wait))
	if !mine {
		return d.JoinWrapUp(ctx, run)
	}
	defer func() { d.Wrap.End(rep) }()

	start := time.Now()
	var cards []*store.Task
	for _, r := range d.Sup.All() {
		t, err := d.St.Get(r.TaskID())
		if err != nil || t == nil {
			continue
		}
		if d.WrapCandidate(t) {
			cards = append(cards, t)
		}
	}
	if len(cards) == 0 {
		log.Printf("[atrium] restart wrap-up: no card is working, nothing to wait for")
		return rep
	}
	sort.Slice(cards, func(i, j int) bool { return cards[i].ID < cards[j].ID })
	log.Printf("[atrium] restart wrap-up (%s): asking %d working card(s) to wrap up, waiting up to %s",
		shared.OrWord(why, "no reason given"), len(cards), wait)

	tctx, tcancel := context.WithTimeout(ctx, wait)
	defer tcancel()
	type result struct {
		T       *store.Task
		Outcome string
		Path    string
	}
	out := make(chan result, len(cards))
	for _, t := range cards {
		path, err := d.PrepareHandoff(t)
		if err != nil {
			log.Printf("[atrium] restart wrap-up: %v. %s is asked without a state file", err, t.DisplayTitle())
			path = d.HandoffPath(t)
		}
		ack := d.Wrap.Expect(t.ID)
		go func(t *store.Task, path string) {
			out <- result{T: t, Outcome: d.WrapCard(tctx, t, path, ack), Path: path}
		}(t, path)
	}
	results := make([]result, 0, len(cards))
	for range cards {
		results = append(results, <-out)
	}
	rep.Cancelled = ctx.Err() != nil
	for _, r := range results {
		d.Wrap.Forget(r.T.ID)
	}
	sort.Slice(results, func(i, j int) bool { return results[i].T.ID < results[j].T.ID })

	for _, r := range results {
		title := r.T.DisplayTitle()
		switch r.Outcome {
		case WrapSkipped:
			log.Printf("[atrium] restart wrap-up: %s settled before it was asked, left alone", title)
			continue
		case WrapReady:
			rep.Ready = append(rep.Ready, title)
		case WrapIdle:
			rep.Idle = append(rep.Idle, title)
		default:
			rep.Unanswered = append(rep.Unanswered, title)
			if rep.Cancelled {
				log.Printf("[atrium] restart wrap-up: %s was cut off, the wait was ended", title)
			} else {
				log.Printf("[atrium] restart wrap-up: %s did not answer within %s", title, wait)
			}
			if err := d.St.AppendEvent(r.T.ID, store.EventNotified, map[string]any{
				"by": WrapBy, "unanswered": true,
				"text": "restart: did not answer the wrap-up in " + wait.String(),
			}); err != nil {
				log.Printf("[atrium] could not record the unanswered wrap-up on %s: %v", r.T.ID, err)
			}
		}
		rep.Asked = append(rep.Asked, title)
		if r.Outcome != WrapUnanswered {
			log.Printf("[atrium] restart wrap-up: %s is %s", title, r.Outcome)
		}
		if d.Wake.Get(r.T.ID) != nil {
			log.Printf("[atrium] restart wrap-up: %s already has a wake of its own, keeping it", title)
			continue
		}
		_, werr := os.Stat(r.Path)
		text := RestartWrapWake(r.Path, werr == nil, r.Outcome)
		if _, _, err := d.QueueWake(r.T.ID, text, WrapBy); err != nil {
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

// joinWrapUp waits for the wrap-up already running instead of starting another, so a second stop does not skip the
// wait. It is bounded by that wrap-up's own deadline plus a margin, and by ctx. It answers with the same outcome.
func (d *Service) JoinWrapUp(ctx context.Context, run *WrapRun) WrapReport {
	limit := time.Until(run.Deadline) + d.Hk.WrapJoinMargin
	log.Printf("[atrium] restart wrap-up: one is already waiting, joining it for up to %s", limit.Round(time.Millisecond))
	timer := time.NewTimer(limit)
	defer timer.Stop()
	start := time.Now()
	select {
	case <-run.Done:
		rep := run.Rep
		rep.Already = true
		log.Printf("[atrium] restart wrap-up: the one already waiting is over after %s, same outcome",
			time.Since(start).Round(time.Millisecond))
		return rep
	case <-timer.C:
		log.Printf("[atrium] restart wrap-up: the one already waiting did not end within %s, going on", limit)
	case <-ctx.Done():
		log.Printf("[atrium] restart wrap-up: no longer joining the one already waiting: %v", ctx.Err())
	}
	return WrapReport{Already: true, WaitedMS: time.Since(start).Milliseconds()}
}

// wrapCard types the prompt into one card and waits for its ack or for it to go idle.
func (d *Service) WrapCard(ctx context.Context, t *store.Task, path string, ack <-chan struct{}) string {
	id := t.ID
	mid := d.MidTurnInputFor(id)
	typed, typedBusy, turn0 := false, false, 0
	tick := time.NewTicker(d.Hk.WrapTiming.Poll)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			if !typed {
				return WrapSkipped
			}
			return WrapUnanswered
		case <-ack:
			return WrapReady
		case <-tick.C:
		}
		run := d.Sup.Get(id)
		if run == nil {
			return WrapSkipped
		}
		if !typed {
			if !d.NcBusy(id) && d.Act.SinceBusy(id) >= d.Hk.NcTiming.TurnSettle {
				return WrapSkipped
			}
			busy, turn := false, 0
			ok := func() bool {
				if d.Act.DialogOpen(id) {
					return false
				}
				busy = d.NcBusy(id)
				turn = d.Act.TurnsBegun(id)
				return mid || !busy
			}
			text := RestartWrapPrompt(path, d.Hk.AtriumBinary())
			wrote, err := d.TypeLabelledGuarded(run, id, WrapLabel, text, ok)
			if err != nil || !wrote {
				continue
			}
			typed, typedBusy, turn0 = true, busy, turn
			log.Printf("[atrium] restart wrap-up: asked %s to wrap up", t.DisplayTitle())
			if err := d.St.AppendEvent(id, store.EventPrompted, map[string]any{
				"text": text, "via": "terminal", "from": WrapBy,
			}); err != nil {
				log.Printf("[atrium] could not record the wrap-up prompt on %s: %v", id, err)
			}
			continue
		}
		// Asked. Idle is a turn that is over and stayed over. A prompt typed between turns must have started one.
		if !d.NcBusy(id) && d.Act.SinceBusy(id) >= d.Hk.NcTiming.TurnSettle &&
			(typedBusy || d.Act.TurnsBegun(id) > turn0) {
			return WrapIdle
		}
	}
}

// handleRestartWrap is POST /v1/restart-wrap: the wrap-up alone, answered when it is over. The hub-asked restart
// calls it before it schedules the restarter. `?wait=` is seconds, else the room's setting.
func (d *Service) HandleRestartWrap(w http.ResponseWriter, r *http.Request) {
	if !d.ShutdownAllowed(w, r) {
		return
	}
	wait := d.St.RestartWrapWait()
	if n := ParseSeconds(r.URL.Query().Get("wait")); n > 0 {
		wait = CapWrapWait(n)
	}
	rep := d.WrapUpForRestart(r.Context(), wait, r.URL.Query().Get("why"))
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(rep)
}

func ParseSeconds(v string) time.Duration {
	var n int
	if _, err := fmt.Sscanf(v, "%d", &n); err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// capWrapWait holds a typed wait to the setting's own maximum, so a caller cannot ask for longer than the room allows.
func CapWrapWait(n time.Duration) time.Duration {
	if max := store.MaxRestartWrapWaitS * time.Second; n > max {
		log.Printf("[atrium] restart wrap-up: a wait of %s is over the limit, using %s", n, max)
		return max
	}
	return n
}
