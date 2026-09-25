package daemon

import (
	"fmt"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The unexpected-exit notice: a line typed into a card whose runner was mid-turn
// when the room went away. See docs/unexpected-exit-wake.md.
//
// A FORCED TURN, ON PURPOSE. The restart wake is typed only because a session
// asked for it. This one is typed because the room took the session's turn away
// from it, by crashing, by being killed, or by a planned stop or a deploy. clint
// decided on 2026-09-25 that a turn the room interrupted is the room's to hand
// back, so the one case "no forced turns" does not cover is atrium's own exit.
//
// MID-TURN IS THE CARD'S STATUS. `running` is a prompt with no Stop yet, and
// `needs-permission` is a turn blocked on a dialog the restart takes away. A card
// waiting in `needs-input` finished its turn and gets nothing.
//
//   - A planned stop reads it in the wind-down, before the runners are stopped,
//     since a runner that exits files its card dead or done.
//   - A crash leaves no wind-down, so the next start reads the statuses the dead
//     room left behind. The wind-down marks the stop, and a start that finds no
//     mark is the one that follows a crash.
//
// DELIVERED AS A RESTART WAKE. The notice is a `restart_wake` row, so it goes
// through the same gate and the same typing path, and one row per card makes the
// precedence: a card's own wake wins, and a notice already waiting is never
// joined by a second.

// exitLabel goes ahead of the notice. See atriumLabel.
var exitLabel = atriumLabel("unexpected exit:")

// Why the room went away, as the notice says it.
const (
	exitRestart = "restart"
	exitCrash   = "crash"
)

// exitNoticeText is the notice for a room that went away at `at`.
func exitNoticeText(why string, at time.Time) string {
	return fmt.Sprintf("atrium went away while you were working (%s) at %s. "+
		"Your session was resumed. Check where you were and carry on.",
		why, at.Local().Format("2006-01-02 15:04:05 MST"))
}

// wasMidTurn reports whether a card's status says a turn was in progress.
func wasMidTurn(t *store.Task) bool {
	return t.Status == store.StatusRunning || t.Status == store.StatusNeedsPermission
}

// queueExitNotice queues the notice on one card and mirrors it.
func (d *Daemon) queueExitNotice(t *store.Task, why string, at time.Time) {
	if t.Throwaway {
		return
	}
	d.wake.deliver.Lock()
	defer d.wake.deliver.Unlock()
	w, queued, err := d.st.QueueUnexpectedExit(t.ID, exitNoticeText(why, at))
	if err != nil {
		log.Printf("[atrium] could not queue the unexpected-exit notice for %s: %v", t.ID, err)
		return
	}
	if !queued {
		// A wake of its own, or a notice from an earlier exit, is already waiting.
		return
	}
	d.wake.put(w)
	log.Printf("[atrium] %s was mid-turn at the %s. it gets an unexpected-exit notice", t.DisplayTitle(), why)
}

// noteStopMidTurn is the planned stop's half. Called from the wind-down before
// any runner is stopped.
func (d *Daemon) noteStopMidTurn() {
	if d.opts.Passive {
		return
	}
	at := time.Now()
	if d.st.UnexpectedExitOn() {
		for _, r := range d.sup.all() {
			t, err := d.st.Get(r.taskID)
			if err != nil || !wasMidTurn(t) {
				continue
			}
			d.queueExitNotice(t, exitRestart, at)
		}
	}
	if err := d.st.MarkRoomStopped(at); err != nil {
		log.Printf("[atrium] could not record the planned stop: %v", err)
	}
}

// noteCrashMidTurn is the crash's half. Called at start, before any runner is
// brought back or the reaper files a card dead.
func (d *Daemon) noteCrashMidTurn() {
	if d.opts.Passive {
		return
	}
	_, planned, err := d.st.TakeRoomStopped()
	if err != nil {
		log.Printf("[atrium] could not read how the last room stopped: %v", err)
		return
	}
	if planned || !d.st.UnexpectedExitOn() {
		return
	}
	tasks, err := d.st.List(store.StatusRunning, store.StatusNeedsPermission)
	if err != nil {
		log.Printf("[atrium] could not look for cards the crash interrupted: %v", err)
		return
	}
	for _, t := range tasks {
		if t.Runner == "" {
			continue
		}
		// A session that outlived the room was never the room's to lose.
		if t.PID > 0 && processAlive(t.PID) {
			continue
		}
		// The last thing heard from the card is the nearest record of when the
		// room went.
		at := t.LastActivityAt
		if at.IsZero() {
			at = time.Now()
		}
		d.queueExitNotice(t, exitCrash, at)
	}
}

// labelFor is the label a waiting row is typed behind.
func labelFor(w *store.RestartWake) string {
	if w.By == store.UnexpectedExitBy {
		return exitLabel
	}
	return wakeLabel
}
