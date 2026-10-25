package daemon

import (
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
	"github.com/dovholuknf/atrium/internal/store"
)

// A card that reads `running` while its terminal shows an idle prompt: the Stop
// hook never landed. See docs/backlog-2.md item 21.
//
// Atrium owns the pty and Claude Code redraws its spinner about once a second
// while it works, so a running card whose pty has been silent for
// LooksIdleAfter and whose last frame is an idle prompt has almost certainly
// finished its turn. That is shown as an ACTIVITY badge and never as a status:
// a column is a bucket of human attention, and a guess must not move a card
// between them. The mark is held in memory with the rest of the activity and
// is cleared by anything the runner or the operator does.

// LooksIdleAfter is how long a pty must be silent before the frame is looked at.
// Twenty-odd missed spinner redraws. The check rides the reaper's tick, so a
// firing lands this long after the turn ended, plus up to one tick.
var LooksIdleAfter = envDuration("ATRIUM_LOOKS_IDLE", 25*time.Second)

// idleMark is one flagged card.
type idleMark struct {
	// at is when it was flagged, which keys the alert so a card that wakes and
	// stalls again rings again.
	at time.Time
	// silent is how long the pty had been quiet when it was decided.
	silent time.Duration
}

// withLooksIdle attaches a flag to an activity, synthesising one when the card
// has none to carry it (the staleness cutoff outlives nothing here, and a lost
// Stop is exactly the case that outlives it). Caller holds the lock.
func (a *activityTracker) withLooksIdle(taskID string, out *Activity) *Activity {
	m, ok := a.looksIdle[taskID]
	if !ok {
		return out
	}
	if out == nil {
		out = &Activity{}
	}
	out.LooksIdle = true
	out.IdleSeconds = int64(m.silent.Seconds())
	out.IdleAt = m.at
	return out
}

// markLooksIdle records the flag, reporting whether it is new.
func (a *activityTracker) markLooksIdle(taskID string, m idleMark) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, ok := a.looksIdle[taskID]; ok {
		return false
	}
	a.looksIdle[taskID] = m
	return true
}

// clearLooksIdle drops the flag, returning what it was.
func (a *activityTracker) clearLooksIdle(taskID string) (idleMark, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m, ok := a.looksIdle[taskID]
	delete(a.looksIdle, taskID)
	return m, ok
}

// looksIdleMark reports a card's flag without changing it.
func (a *activityTracker) looksIdleMark(taskID string) (idleMark, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	m, ok := a.looksIdle[taskID]
	return m, ok
}

// lastOutputAt is when the pty last produced output, or when the runner started
// if it has produced none.
func (r *runner) lastOutputAt() time.Time {
	if n := r.lastOut.Load(); n != 0 {
		return time.Unix(0, n)
	}
	return r.started
}

// watchLooksIdle is one pass over the running cards, on the reaper's tick. It
// reads the runner and the in-memory activity, never writes the store, and never
// touches a runner beyond a bounded read of its last frame.
//
// Claude only: no other runner has an idle signature that has been confirmed,
// and a badge that claims idleness from a guess about somebody else's screen
// is worse than none.
func (d *Daemon) watchLooksIdle(now time.Time) error {
	d.trustReattachedLines(now)
	tasks, err := d.st.List(store.StatusRunning)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		if !strings.EqualFold(t.Runner, "claude") {
			continue
		}
		run := d.sup.get(t.ID)
		if run == nil {
			continue
		}
		if _, on := d.act.looksIdleMark(t.ID); on {
			continue
		}
		if !d.act.midTurn(t.ID) || d.act.onSubagents(t.ID) || d.act.dialogOpen(t.ID) {
			continue
		}
		silent := now.Sub(run.lastOutputAt())
		if silent < LooksIdleAfter {
			continue
		}
		cols, rows := run.buf.CurrentSize()
		idle, why := screen.ClassifyFrame(run.buf.Tail(screen.FrameTailBytes), cols, rows)
		if !idle {
			continue
		}
		d.flagLooksIdle(t, run, silent, why, now)
	}
	return nil
}

// reattachQuiet and reattachQuietOther are how long a reattached runner's terminal must have been silent before its
// screen is trusted to say the line is empty. Claude's frame is read exactly, so a few seconds is enough. Anything
// else has no confirmed idle signature, so it waits longer and is read for the one thing every runner draws while it
// works, an interrupt hint.
var (
	reattachQuiet      = envDuration("ATRIUM_REATTACH_QUIET", 5*time.Second)
	reattachQuietOther = envDuration("ATRIUM_REATTACH_QUIET_OTHER", 30*time.Second)
)

// trustReattachedLines is trustReattachedLine for every supervised runner, in whatever column its card sits in: an
// idle worker is needs-input or done, never running, and it is the one a say must reach.
func (d *Daemon) trustReattachedLines(now time.Time) {
	for _, run := range d.sup.all() {
		d.trustReattachedLine(run, now)
	}
}

// trustReattachedLine reads the screen of a runner picked up from the pty host whose typed line is unsure only
// because of the restart. A quiet terminal with nothing on its input line proves the line empty without a keystroke,
// and a Stop hook lost while the room was down would otherwise hold every typed prompt and queued say until a human
// pressed Enter. A line with anything on it, a key typed since the reattach, a dialog and a runner mid-turn all stay
// unsure.
func (d *Daemon) trustReattachedLine(run *runner, now time.Time) {
	if !run.reattached.Load() || run.buf == nil || d.act.dialogOpen(run.taskID) || d.act.onSubagents(run.taskID) {
		return
	}
	run.typeMu.Lock()
	unsure := run.line.unsure == reattachLineKey && run.keySeq == 0
	run.typeMu.Unlock()
	if !unsure {
		return
	}
	t, err := d.st.Get(run.taskID)
	if err != nil {
		return
	}
	claude := strings.EqualFold(t.Runner, "claude")
	need := reattachQuietOther
	if claude {
		need = reattachQuiet
	}
	if now.Sub(run.lastOutputAt()) < need {
		return
	}
	cols, rows := run.buf.CurrentSize()
	tail := run.buf.Tail(screen.FrameTailBytes)
	if claude {
		if !screen.FramePromptEmpty(tail, cols, rows) && !screen.FrameLineEmpty(tail, cols, rows) {
			return
		}
	} else if d.act.midTurn(run.taskID) || screen.FrameHasInterrupt(tail, cols, rows) {
		return
	}
	run.typeMu.Lock()
	if run.line.unsure != reattachLineKey || run.keySeq != 0 {
		run.typeMu.Unlock()
		return
	}
	run.line.clear()
	run.typeMu.Unlock()
	log.Printf("[atrium] %s: reattached and quiet for %s, and the screen shows no work in progress, so the line is taken as empty",
		run.taskID, need)
	if d.pending != nil {
		d.pending.reset(run.taskID)
	}
}

// flagLooksIdle sets the badge, arms the runner to take it down again, and logs.
func (d *Daemon) flagLooksIdle(t *store.Task, run *runner, silent time.Duration, why string, now time.Time) {
	silent = silent.Truncate(time.Second)
	if !d.act.markLooksIdle(t.ID, idleMark{at: now, silent: silent}) {
		return
	}
	d.looksIdleFired.Add(1)
	what, age := d.act.describe(t.ID, now)
	log.Printf("[atrium] looks idle: %s (%s) silent %s, frame=%s, activity %s, no turn-end received (%d so far)",
		t.WireName, t.ID, silent, why, what+age, d.looksIdleFired.Load())
	wake := func(cause string) {
		// Off the pty reader and the keystroke path: this publishes.
		run.wake.Store(nil)
		go d.looksIdleGone(t.ID, t.WireName, cause)
	}
	run.wake.Store(&wake)
	// Messages held for a turn end that already happened are retried now, not on the backoff.
	if d.pending != nil {
		d.pending.reset(t.ID)
	}
	d.publishTask(t.ID)
	d.ap.Broadcast("activity", map[string]any{"task_id": t.ID, "activity": d.act.get(t.ID)})
}

// looksIdleGone takes the flag down, because something happened. A no-op when it
// is already down, which is what a hook event leaves behind: `set` clears it.
func (d *Daemon) looksIdleGone(taskID, wire, cause string) {
	m, ok := d.act.clearLooksIdle(taskID)
	if !ok {
		return
	}
	log.Printf("[atrium] looks idle cleared: %s (%s) by %s after %s", wire, taskID, cause,
		time.Since(m.at).Truncate(time.Second))
	d.publishTask(taskID)
	if a := d.act.get(taskID); a != nil {
		d.ap.Broadcast("activity", map[string]any{"task_id": taskID, "activity": a})
	}
}

// describe is the activity's state and age for a log line, past the staleness
// cutoff, with the age already spaced.
func (a *activityTracker) describe(taskID string, now time.Time) (string, string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		return "none", ""
	}
	what := cur.What
	if cur.Tool != "" {
		what += " " + cur.Tool
	}
	return what, " for " + now.Sub(cur.Since).Truncate(time.Second).String()
}
