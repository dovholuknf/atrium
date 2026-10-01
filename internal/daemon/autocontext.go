package daemon

import (
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// The automatic new context: when a card's context passes a threshold, atrium runs
// the new-context cycle on it. See docs/runtime/auto-new-context-design.md, which is
// the whole argument. The cycle itself is newcontext.go and is unchanged by this
// except for the few places an automatic run differs (`newContext.auto`).
//
// IT DISCARDS A LIVE CONVERSATION, so it is off unless switched on, and stricter than
// anything else that acts on a card without being asked: a card a person works in is
// never cycled unless someone tagged that card, and then only with nobody watching
// and a long quiet.
//
// THE ARM STATE IS IN MEMORY, like activity and the cycle itself. It describes a
// process running now. A restart meets a still large card cold, waits out
// `autoTiming.startGrace`, and judges it again, which can give one extra cycle across
// a restart and is what a person pressing the button would get.

const (
	// AutoContextTag opts a card in, under `tagged` and under `agents`.
	AutoContextTag = "atrium:auto-new-context"
	// NoAutoContextTag always excludes a card, whatever else is set.
	NoAutoContextTag = "atrium:no-auto-new-context"
	// ContextCeilingTag holds a card to `context_ceiling_k`, whatever the mode says, and
	// lets the cycle start on it mid-turn. It is put on directors, by the orchestrator.
	ContextCeilingTag = "atrium:context-ceiling"
	// NoticeAutoContext is the launcher's notice for this: one when a run begins, one
	// when it gives up, one when it left the card large. Keyed in the store per
	// session, so a restart does not say it twice.
	NoticeAutoContext = "auto-new-context"
	// autoContextBy is the `by` on the events an automatic run writes. It is atrium,
	// never a person or the launcher.
	autoContextBy = "auto-new-context"
)

// autoTiming holds what is a constant in production. Variables, so a test runs them in
// milliseconds as it does ncTiming.
var autoTiming = struct {
	// startGrace: nothing fires this long after the daemon starts.
	startGrace time.Duration
	// minGap is the least between two automatic cycles on one card.
	minGap time.Duration
	// retryAfter is how long after a failed attempt the one retry waits.
	retryAfter time.Duration
	// typeWait is how long the FIRST typing waits for an empty line before the attempt
	// is dropped, silently, to be tried again on a later tick.
	typeWait time.Duration
	// humanQuiet is how long a tagged human card must have been quiet.
	humanQuiet time.Duration
	// idleUnit is what `auto_new_context_idle_s` is counted in.
	idleUnit time.Duration
}{
	startGrace: 5 * time.Minute,
	minGap:     30 * time.Minute,
	retryAfter: 30 * time.Minute,
	typeWait:   2 * time.Minute,
	humanQuiet: 10 * time.Minute,
	idleUnit:   time.Second,
}

// The arm states.
const (
	autoArmed  = "armed"
	autoFired  = "fired"
	autoGaveUp = "gave-up"
)

// autoState is one card's memory of its automatic cycles.
type autoState struct {
	state string
	// conv is the conversation the cycle began in. The card is proven to have moved on
	// when its conversation is another one.
	conv    string
	firedAt time.Time
	// attempts is how many automatic attempts this crossing has had, at most two.
	attempts int
	// retryAt is when the one retry is due, or zero. wakeOnly says it is the wake alone.
	retryAt  time.Time
	wakeOnly bool
	// finished: the wake was typed. resultNoted: the result was written. largeNoted:
	// the card was told it came back large.
	finished, resultNoted, largeNoted bool
	tokens, threshold                 int64
	gen                               uint64
	file                              string
}

type autoContexts struct {
	mu sync.Mutex
	by map[string]*autoState
	// born is when this daemon began, for the restart grace.
	born time.Time
}

func newAutoContexts() *autoContexts {
	return &autoContexts{by: map[string]*autoState{}, born: time.Now()}
}

func (a *autoContexts) get(id string) *autoState {
	a.mu.Lock()
	defer a.mu.Unlock()
	if s := a.by[id]; s != nil {
		cp := *s
		return &cp
	}
	return nil
}

// update changes a card's state, creating it armed when there is none.
func (a *autoContexts) update(id string, f func(*autoState)) {
	a.mu.Lock()
	defer a.mu.Unlock()
	s := a.by[id]
	if s == nil {
		s = &autoState{state: autoArmed}
		a.by[id] = s
	}
	f(s)
}

// forgetExcept drops the cards no longer in open, which are gone or parked.
func (a *autoContexts) forgetExcept(open map[string]bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	for id := range a.by {
		if !open[id] {
			delete(a.by, id)
		}
	}
}

// autoContextSubject is whether the setting reaches a card at all. Never a fixture, a
// throwaway or a card with a lent session, whatever the setting.
//
// A card wearing ContextCeilingTag is subject whatever the mode, `off` included: the
// ceiling is its own switch.
func (d *Daemon) autoContextSubject(t *store.Task, mode string) bool {
	if t == nil || t.Throwaway || d.fixtureCards()[t.ID] || d.guests.get(t.ID) != nil {
		return false
	}
	if hasTag(t.Tags, NoAutoContextTag) {
		return false
	}
	return hasTag(t.Tags, ContextCeilingTag) || autoModeSubject(t, mode)
}

// autoModeSubject is whether the global mode alone reaches a card.
func autoModeSubject(t *store.Task, mode string) bool {
	if mode == store.AutoNewContextOff {
		return false
	}
	if hasTag(t.Tags, AutoContextTag) {
		return true
	}
	return mode == store.AutoNewContextAgents && hasTag(t.Tags, OriginAgentTag) && !hasTag(t.Tags, SubagentTag)
}

// autoThreshold is the size at which a card is cycled, in tokens: the setting, or 70
// percent of the window its statusline last reported, whichever is lower. The
// statusline is used for the window and nothing else. The 30 percent left is room for
// the capture turn, which costs context and must not be the turn that compacts.
//
// A ceiling card is held to the ceiling, and to the global line as well when the mode
// reaches it too: the lower of the two.
func (d *Daemon) autoThreshold(t *store.Task) int64 {
	limit := int64(api.EffectiveAutoNewContextK(d.st)) * 1000
	if hasTag(t.Tags, ContextCeilingTag) {
		ceiling := int64(api.EffectiveContextCeilingK(d.st)) * 1000
		if !autoModeSubject(t, d.st.AutoNewContextMode()) || ceiling < limit {
			limit = ceiling
		}
	}
	if tel := d.act.telemetry(t.ID); tel != nil && tel.Window > 0 {
		if w := int64(tel.Window) * 70 / 100; w < limit {
			limit = w
		}
	}
	return limit
}

// autoHuman is a card no agent launched. It is subject only when tagged. A ceiling card is
// never one: the tag is put on directors, which an agent launched, and gets the agent rule.
func autoHuman(t *store.Task) bool {
	return !hasTag(t.Tags, OriginAgentTag) && !hasTag(t.Tags, ContextCeilingTag)
}

// autoReady is the state gates of design section 4, read at the tick.
//
// A ceiling card is not held back for being mid-turn or running, since a director sits in
// long turns and a gate that waits for a prompt never opens on one. The capture asks it to
// end the turn instead. Every other gate holds. The idle quiet is a fact about a card
// between turns, so it cannot be asked of one in a turn.
func (d *Daemon) autoReady(t *store.Task, human bool, now time.Time) bool {
	id := t.ID
	ceiling := hasTag(t.Tags, ContextCeilingTag)
	run := d.sup.get(id)
	if run == nil || isParked(t) {
		return false
	}
	if t.Status != store.StatusNeedsInput && !(ceiling && t.Status == store.StatusRunning) {
		return false
	}
	if perms, err := d.st.PendingForTask(id); err != nil || len(perms) > 0 {
		return false
	}
	busy := d.act.midTurn(id) || d.cardRunning(id)
	if (busy && !ceiling) || d.act.dialogOpen(id) || d.act.onSubagents(id) {
		return false
	}
	if n, _ := d.act.backgroundWork(id); n > 0 {
		return false
	}
	if human {
		if run.watching() || now.Sub(d.rawIdleSince(t)) < autoTiming.humanQuiet {
			return false
		}
	} else if !(ceiling && busy) &&
		d.act.sinceBusy(id) < time.Duration(d.st.AutoNewContextIdleS())*autoTiming.idleUnit {
		return false
	}
	if msgs, err := d.st.PendingMessages(id); err != nil || len(msgs) > 0 {
		return false
	}
	if d.wake.get(id) != nil || d.nctx.holding(id) {
		return false
	}
	if m := d.idle.get(id); m != nil && m.capturing {
		return false
	}
	return d.nctx.sameDirBusy(d, t) == ""
}

// watchAutoContext is called by watchContext for each live Claude card with the size
// it just read, on every tick and not only when the size changed: the gates move on
// their own.
func (d *Daemon) watchAutoContext(t *store.Task, tokens int64, now time.Time) {
	threshold := d.autoThreshold(t)
	s := d.settleAuto(t, tokens, threshold)
	mode := d.st.AutoNewContextMode()
	if !d.autoContextSubject(t, mode) || d.sup.get(t.ID) == nil {
		return
	}
	if now.Sub(d.auto.born) < autoTiming.startGrace {
		return
	}
	human := autoHuman(t)
	switch {
	case s != nil && s.state == autoFired && !s.retryAt.IsZero():
		if now.Before(s.retryAt) || (!s.wakeOnly && tokens < threshold) {
			return
		}
		// A retry is the same crossing, so the minimum gap does not apply to it. The
		// wake alone is not held to the human's quiet: the context is already gone.
		if d.autoReady(t, human && !s.wakeOnly, now) {
			d.startAuto(t, tokens, threshold, human, s.wakeOnly, s.attempts+1)
		}
	case s == nil || s.state == autoArmed:
		if tokens < threshold || (s != nil && !s.firedAt.IsZero() && now.Sub(s.firedAt) < autoTiming.minGap) {
			return
		}
		if d.autoReady(t, human, now) {
			d.startAuto(t, tokens, threshold, human, false, 1)
		}
	}
}

// autoRetryUnread is the tick for a card that reads as nothing: cleared, and its new
// conversation has no reply yet. Only the wake retry is due there, and it is the retry
// that gets the card a reply, so it cannot wait for a size to read.
func (d *Daemon) autoRetryUnread(t *store.Task, now time.Time) {
	s := d.auto.get(t.ID)
	if s == nil || s.state != autoFired || s.retryAt.IsZero() || !s.wakeOnly {
		return
	}
	if cur := d.nctx.get(t.ID); cur == nil || !cur.auto || cur.step != NewContextFailed {
		// The chip was dismissed or replaced. A person chose, so the retry is off.
		d.auto.update(t.ID, func(s *autoState) { s.state, s.attempts, s.retryAt = autoArmed, 0, time.Time{} })
		return
	}
	if !d.autoContextSubject(t, d.st.AutoNewContextMode()) || d.sup.get(t.ID) == nil ||
		now.Sub(d.auto.born) < autoTiming.startGrace || now.Before(s.retryAt) {
		return
	}
	if d.autoReady(t, false, now) {
		d.startAuto(t, s.tokens, s.threshold, autoHuman(t), true, s.attempts+1)
	}
}

// settleAuto moves a card's arm state on what the tick sees, and returns it. Nothing
// here depends on the setting: a run in flight is finished being recorded whatever the
// setting says now.
//
//   - A finished run whose next context read has come back writes its result, once.
//   - FIRED goes back to ARMED only when the card is in another conversation AND under
//     half the line. One that is in another conversation and still large stays FIRED,
//     and says so once.
//   - GAVE_UP, and a retry nobody is waiting on, end when a person acted on the chip:
//     a press, a dismissal or a new conversation.
func (d *Daemon) settleAuto(t *store.Task, tokens, threshold int64) *autoState {
	s := d.auto.get(t.ID)
	if s == nil {
		return nil
	}
	cur := d.nctx.get(t.ID)
	conv := d.conversationOf(t)
	moved := s.conv != "" && conv != "" && conv != s.conv
	switch s.state {
	case autoGaveUp:
		if cur == nil || !cur.auto || cur.step != NewContextFailed || moved {
			d.auto.update(t.ID, func(s *autoState) { s.state, s.attempts, s.retryAt = autoArmed, 0, time.Time{} })
			return d.auto.get(t.ID)
		}
	case autoFired:
		if !s.retryAt.IsZero() && (cur == nil || !cur.auto || cur.step != NewContextFailed) && !moved {
			// The chip was dismissed or replaced while a retry was waiting. A person
			// chose, so the retry is off. The minimum gap still holds it back.
			d.auto.update(t.ID, func(s *autoState) { s.state, s.attempts, s.retryAt = autoArmed, 0, time.Time{} })
			return d.auto.get(t.ID)
		}
		if cur == nil && !s.finished && s.retryAt.IsZero() && !moved {
			// The run was dismissed from the board before it ended. Nothing is
			// running, and the minimum gap still holds it back.
			d.auto.update(t.ID, func(s *autoState) { s.state, s.attempts = autoArmed, 0 })
			return d.auto.get(t.ID)
		}
		if !moved {
			return s
		}
		if s.finished && !s.resultNoted {
			if err := d.st.AppendEvent(t.ID, store.EventNotified, map[string]any{
				"by": autoContextBy, "done": true, "before": s.tokens, "after": tokens,
			}); err != nil {
				log.Printf("[atrium] could not record the automatic new context result on %s: %v", t.ID, err)
			}
			d.auto.update(t.ID, func(s *autoState) { s.resultNoted = true })
		}
		if tokens < threshold/2 {
			d.auto.update(t.ID, func(s *autoState) { s.state, s.attempts, s.retryAt = autoArmed, 0, time.Time{} })
			return d.auto.get(t.ID)
		}
		if s.finished && !s.largeNoted {
			d.auto.update(t.ID, func(s *autoState) { s.largeNoted = true })
			if d.reportsToLauncher(t) {
				d.notifyLauncher(t, NoticeAutoContext, "large:"+conv, fmt.Sprintf(
					"auto new context ran and %s is still at %dk, its handoff is probably too large.",
					t.WireName, tokens/1000))
			}
		}
	}
	return d.auto.get(t.ID)
}

// startAuto claims the card and starts a run on its own goroutine. The claim is the
// same atomic `begin` a button press makes, so a tick and a press cannot both run.
func (d *Daemon) startAuto(t *store.Task, tokens, threshold int64, human, wakeOnly bool, attempt int) {
	id := t.ID
	if d.sup.get(id) == nil {
		return
	}
	file := HandoffName(t)
	gen, ok := d.nctx.beginAuto(id, file, d.conversationOf(t), tokens, threshold, human, wakeOnly,
		hasTag(t.Tags, ContextCeilingTag))
	if !ok {
		return
	}
	now := time.Now()
	d.auto.update(id, func(s *autoState) {
		if !wakeOnly {
			s.conv = d.conversationOf(t)
		}
		s.state, s.firedAt, s.attempts, s.retryAt, s.wakeOnly = autoFired, now, attempt, time.Time{}, false
		s.finished, s.resultNoted, s.largeNoted = false, false, false
		s.tokens, s.threshold, s.gen, s.file = tokens, threshold, gen, file
	})
	d.autoIdleHold(t)
	log.Printf("[atrium] auto new context started on %s at %dk (line %dk, attempt %d)",
		t.DisplayTitle(), tokens/1000, threshold/1000, attempt)
	d.publishTask(id)
	go d.runNewContext(id, gen)
}

// autoPrepare is the front of an automatic run, before anything is typed. It waits for
// an empty line, for at most `autoTiming.typeWait`, and stands aside when a person is
// using the card: nothing has been typed and nothing is lost. A card whose terminal
// went, or that is no longer subject, is the same. It then TELLS THE LAUNCHER, which is
// the notice this design rests on: it is sent before the capture prompt is typed, so the
// launcher hears before anything is cleared, whichever of the two numbers is lower.
func (d *Daemon) autoPrepare(taskID string, gen uint64) error {
	err := d.ncWait(taskID, gen, autoTiming.typeWait, "an empty line", func() (bool, error) {
		run := d.sup.get(taskID)
		return run != nil && run.peerGateOpen() && d.autoStillOK(taskID), nil
	})
	if err != nil {
		return err
	}
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	s := d.auto.get(taskID)
	if s == nil || s.gen != gen {
		return errNewContextGone
	}
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
		"by": autoContextBy, "started": true, "tokens": s.tokens, "threshold": s.threshold,
		"file": s.file, "attempt": s.attempts,
	}); err != nil {
		log.Printf("[atrium] could not record the automatic new context start on %s: %v", taskID, err)
	}
	if d.reportsToLauncher(t) {
		at := "reached"
		if cur := d.nctx.get(taskID); cur != nil && cur.ceiling {
			at = "passed its context ceiling at"
		}
		d.notifyLauncher(t, NoticeAutoContext, d.ctx.sessionOf(t), fmt.Sprintf(
			"%s %s %dk and atrium is cycling its context (handoff %s). It will wake and read it, no action needed.",
			t.WireName, at, s.tokens/1000, s.file))
	}
	return nil
}

// autoStillOK is the check made again just before typing: the card is still subject,
// and a human card still has nobody at its terminal.
func (d *Daemon) autoStillOK(taskID string) bool {
	t, err := d.st.Get(taskID)
	if err != nil || !d.autoContextSubject(t, d.st.AutoNewContextMode()) {
		return false
	}
	if autoHuman(t) {
		run := d.sup.get(taskID)
		return run != nil && !run.watching()
	}
	return true
}

// autoDeferred ends a run that typed nothing: no chip, no failure, no event. The card
// goes back to armed and a later tick tries again.
func (d *Daemon) autoDeferred(taskID string, gen uint64) {
	if !d.nctx.finish(taskID, gen) {
		return
	}
	d.auto.update(taskID, func(s *autoState) {
		if s.gen == gen {
			s.state, s.attempts = autoArmed, 0
			// The gap is for cycles that ran. Nothing did.
			s.firedAt = time.Time{}
		}
	})
	d.autoIdleRelease(taskID, false)
	d.publishTask(taskID)
	d.releaseHeld(taskID)
}

// autoFailing decides what a failed step of an automatic run leads to, and returns the
// reason the chip should carry, the attempt it was and whether it gives up.
//
//   - Before anything was cleared (capture, or `/clear` not typed): one retry after
//     `retryAfter`, then give up.
//   - `/clear` typed and no new session: the state is unknown and only a SessionStart
//     proves it. NEVER retried. A person decides.
//   - Cleared and the wake failed: the context is gone and the wake is the only step
//     that puts the card back, so the wake ALONE is retried once.
func (d *Daemon) autoFailing(taskID string, gen uint64, stage, step, reason string) (string, int, bool) {
	attempt, giveUp := 0, false
	d.auto.update(taskID, func(s *autoState) {
		if s.gen != gen {
			return
		}
		attempt = s.attempts
		unproven := stage == NewContextClear && step == "the context did not clear"
		if unproven || attempt >= 2 {
			s.state, s.retryAt = autoGaveUp, time.Time{}
			giveUp = true
			return
		}
		s.retryAt, s.wakeOnly = time.Now().Add(autoTiming.retryAfter), stage == NewContextWake
	})
	if giveUp {
		return reason + ". Not retrying, press New context to try again", attempt, true
	}
	return reason + ". It will try once more", attempt, false
}

// autoGaveUpNotice tells the launcher of an agent card that atrium stopped, once per session.
// A human card has the chip and no notice.
func (d *Daemon) autoGaveUpNotice(taskID, reason string) {
	t, err := d.st.Get(taskID)
	if err != nil || !d.reportsToLauncher(t) {
		return
	}
	tokens, _ := d.ctx.read(t)
	d.notifyLauncher(t, NoticeAutoContext, "failed:"+d.ctx.sessionOf(t), fmt.Sprintf(
		"%s is at %dk and atrium could not cycle its context: %s. Not retrying.", t.WireName, tokens/1000, reason))
}

// autoFinished marks the wake typed, or takes the mark back when the chip was
// dismissed first. The result is written by the tick, when the next context read from
// the new conversation comes back.
func (d *Daemon) autoFinished(taskID string, gen uint64, done bool) {
	d.auto.update(taskID, func(s *autoState) {
		if s.gen == gen {
			s.finished = done
		}
	})
}

// autoIdleHold keeps the cycle's own prompts from moving the card's idle clock: a
// cycle is atrium's work and not the card's (decided question 3). It is the mark idle
// parking takes for its own capture, so the same filter applies. A handoff idle
// parking already took is kept under it, for a failed cycle to put back.
func (d *Daemon) autoIdleHold(t *store.Task) {
	m := &handoffMark{base: d.idleSince(t), capturing: true, file: HandoffName(t)}
	if prev := d.idle.get(t.ID); prev != nil && !prev.capturing {
		prev.prev = nil
		m.prev = prev
	}
	d.idle.put(t.ID, m)
}

// autoIdleRelease ends the hold. A cycle that got through leaves the mark as a handoff
// taken, with its clock still the old one. A failed one leaves the mark it found.
func (d *Daemon) autoIdleRelease(taskID string, ok bool) {
	m := d.idle.get(taskID)
	if m == nil {
		return
	}
	if !ok {
		if m.prev != nil {
			d.idle.put(taskID, m.prev)
		} else {
			d.idle.drop(taskID)
		}
		return
	}
	m.capturing, m.end, m.written, m.prev = false, time.Now(), true, nil
	d.idle.put(taskID, m)
}
