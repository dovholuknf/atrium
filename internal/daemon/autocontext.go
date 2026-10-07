package daemon

import (
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// THE ONE TRIGGER: a card past its context limit starts the context cycle, exactly as if someone pressed
// New context on it. See docs/runtime/context-cycle-design.md and newcontext.go for the sequence.
//
// The limit is the card's own, else the hub's for its harness, else claude at 200k (api.ContextLimitFor).
// Every supervised card is covered, a person's own included, unless its details switch it off. Nothing else
// excludes a card: no tag, no mode, no human or agent split. A lent session stays out, since it is not work
// this room owns. A fixture is cycled like any card: it runs as long as the daemon does.
//
// Checked on the reaper's tick and on every statusline update (onTelemetry). A cycle already on the card is
// poked instead, so a prompt that is due goes at once.

// cycleBy is the `by` on the events the context cycle writes.
const cycleBy = "context-cycle"

// cycleSubject is whether the trigger reaches a card at all.
func (d *Daemon) cycleSubject(t *store.Task) bool {
	if t == nil || d.sup.get(t.ID) == nil || isParked(t) || !api.ContextCycleOn(t) {
		return false
	}
	switch t.Status {
	case store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission:
	default:
		return false
	}
	if d.guests.get(t.ID) != nil || d.frozenForMove(t.ID) {
		return false
	}
	return true
}

// cycleLimit is the card's limit in tokens, or 0 for none.
func (d *Daemon) cycleLimit(t *store.Task) int64 {
	k, _ := api.ContextLimitFor(d.st, t)
	return int64(k) * 1000
}

// cycleCheck starts a cycle on a card past its limit, or pokes the one already on it. A failed chip holds the
// trigger off until it is dismissed, rerun or the clear is proven, so a broken cycle is not started again on
// every tick.
func (d *Daemon) cycleCheck(t *store.Task, tokens int64) {
	if cur := d.nctx.get(t.ID); cur != nil {
		if cur.step != NewContextFailed {
			d.nctx.kick(t.ID)
		}
		return
	}
	if !d.cycleSubject(t) {
		return
	}
	limit := d.cycleLimit(t)
	if limit <= 0 || tokens < limit {
		return
	}
	d.startCycle(t, tokens, limit)
}

// cycleOnStatusline is the check for a card whose statusline just posted. The size is the transcript's, read
// again (a stat when it has not changed), so the trigger and the board agree on it.
func (d *Daemon) cycleOnStatusline(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil || t == nil {
		return
	}
	if tokens, _ := d.ctx.read(t); tokens > 0 {
		d.cycleCheck(t, tokens)
	}
}

// startCycle claims the card for an automatic cycle and runs it. The claim is the same one a button press
// makes, so a tick and a press cannot both run.
func (d *Daemon) startCycle(t *store.Task, tokens, limit int64) {
	path, err := d.prepareHandoff(t)
	if err != nil {
		log.Printf("[atrium] context cycle on %s not started: %v", t.DisplayTitle(), err)
		return
	}
	gen, ok := d.nctx.claim(t.ID, &newContext{step: NewContextLimit, file: path, conv: d.conversationOf(t),
		auto: true, tokens: tokens, limit: limit})
	if !ok {
		return
	}
	log.Printf("[atrium] context cycle started on %s at %dk (limit %dk)", t.DisplayTitle(), tokens/1000, limit/1000)
	if err := d.st.AppendEvent(t.ID, store.EventNotified, map[string]any{
		"by": cycleBy, "started": true, "tokens": tokens, "limit": limit, "path": path,
	}); err != nil {
		log.Printf("[atrium] could not record the context cycle start on %s: %v", t.ID, err)
	}
	d.publishTask(t.ID)
	go d.runNewContext(t.ID, gen)
}

// cardLimit is the size atrium holds a card to, in tokens, for the runner's compaction window. A card with no
// limit is given claude's default, which only sizes the backstop.
func (d *Daemon) cardLimit(t *store.Task) int64 {
	if limit := d.cycleLimit(t); limit > 0 {
		return limit
	}
	return int64(store.DefaultContextLimits()["claude"]) * 1000
}

// The range the runner takes for its compaction window, in thousands of tokens.
const (
	minAutocompactK = 100
	maxAutocompactK = 1000
)

// compactReserveK is how far below its --autocompact value claude really compacts, in thousands of tokens: it
// keeps room for the summary it writes. Measured on claude 2.1.288: a flag of 330k compacted at about 297k and a
// flag of 165k at about 133k, so 33k, which is the 20k summary reserve and 13k buffer claude takes off the window.
const compactReserveK = 33

// autocompactK is the compaction window a card's runner is started with, in thousands of
// tokens. Claude compacts compactReserveK below the value it is given, so the value is the
// card's limit plus 10 percent plus that reserve: the runner then compacts 10 percent AFTER
// atrium's cycle point, as the backstop, and never before it. Clamped to what claude
// accepts, and to the model's own window when it is known, where the real compaction
// comes out lower than asked. A limit under 58k is raised to the floor, which is late
// rather than early. One function, for the launch, the card details and the peek, whose
// caption is compactsAtK.
func (d *Daemon) autocompactK(t *store.Task) int {
	k := int(d.cardLimit(t)*11/10/1000) + compactReserveK
	// Never past the model's own window, when it is known: a window above it is accepted
	// but not known to compact in time. An unnamed model is left to the range alone.
	if w := modelWindowK(t.Model); w > 0 && k > w {
		k = w
	}
	if k < minAutocompactK {
		return minAutocompactK
	}
	if k > maxAutocompactK {
		return maxAutocompactK
	}
	return k
}

// compactsAtK is where the session will really compact, in thousands of tokens: the launch value less
// claude's reserve. Derived from autocompactK, so the peek and the launch cannot disagree.
func (d *Daemon) compactsAtK(t *store.Task) int {
	return d.autocompactK(t) - compactReserveK
}

// Autocompact is the card details' numbers: the limit, the value the runner is started with
// and where it really compacts.
type Autocompact struct {
	LimitK  int `json:"limit_k"`
	WindowK int `json:"window_k,omitempty"`
	// CompactsK is where the session really compacts, which is what the peek shows.
	CompactsK int `json:"compacts_k,omitempty"`
	// Note is set when the runner does not take the flag.
	Note string `json:"note,omitempty"`
}

// autocompactFor is what the board shows for a card, nil when its runner takes no
// compaction flag.
func (d *Daemon) autocompactFor(t *store.Task) any {
	h, err := d.st.Harness(t.Runner)
	if err != nil || h == nil || len(h.AutocompactArgs) == 0 {
		return nil
	}
	if d.autocompactArgsFor(h) == nil {
		return &Autocompact{LimitK: int(d.cardLimit(t) / 1000), Note: NoAutocompactNote}
	}
	return &Autocompact{LimitK: int(d.cardLimit(t) / 1000), WindowK: d.autocompactK(t), CompactsK: d.compactsAtK(t)}
}

// cycleTimeNow is the clock the cycle reads. A variable for a test.
var cycleTimeNow = time.Now
