package daemon

import (
	"fmt"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card with no session behind it: finished or dead, and nothing live to talk to.
//
// A SAY TO ONE IS UNDELIVERABLE, NOT QUEUED. It used to answer `queued`, be held
// for the input line, and wear `! 1` blaming that line for as long as anybody
// cared to look, on a card with no line to wait on. Nothing can read it until
// the session is resumed, and a resumed session is the sender's to start, so the
// sender is told that now and says it again after. It is not queued as well, or
// the resumed session would get it twice.
//
// WHAT COUNTS AS SOMEBODY TO TALK TO. A card in `done` or `dead` is gone unless
// something is still there, and there are two ways something is:
//
//   - the recorded pid is alive, which is what this used to be the only test of.
//   - atrium owns a runner for the card whose process has not exited, AND the
//     session on it has not ended.
//
// The second is item 83. A worker that reports `done` is filed in `done` and keeps
// running at its prompt so a director can send it review changes. The card's pid
// is a hint the session hook wrote, and it can be 0 or a process that is not the
// runner, so the record alone called a live terminal gone and the say bounced.
// The supervisor is who holds the pty, so it is asked too.
//
// `sessionEnded` keeps the old rule for a runner that OUTLIVES its session: after
// a SessionEnd hook the card is done, and the pty can stay registered for a while
// as its process winds down, and that is not somebody to talk to. Every path that
// ends a session writes an `exited` event, so that is the record consulted, not
// the card's column.
func (d *Daemon) sessionGone(t *store.Task) bool {
	if t.Status != store.StatusDone && t.Status != store.StatusDead {
		return false
	}
	if t.PID > 0 && processAlive(t.PID) {
		return false
	}
	return !d.runnerLive(t)
}

// runnerLive reports whether atrium owns a terminal for the card whose process is
// still running and whose session has not ended.
func (d *Daemon) runnerLive(t *store.Task) bool {
	run := d.sup.get(t.ID)
	if run == nil {
		return false
	}
	select {
	case <-run.done:
		return false
	default:
	}
	return !d.sessionEnded(t.ID)
}

// sessionEnded reports whether the newest of the card's launch and exit events is
// an exit. A store error answers true: the refusal that leads to is the one already
// in force for every card with no process, and it says to resume.
func (d *Daemon) sessionEnded(taskID string) bool {
	evs, err := d.st.Events(taskID, 200)
	if err != nil {
		return true
	}
	for i := len(evs) - 1; i >= 0; i-- {
		switch evs[i].Kind {
		case store.EventExited:
			return true
		case store.EventLaunched:
			return false
		}
	}
	return false
}

// goneNote is what the sender of an undeliverable say is told.
func goneNote(t *store.Task) string {
	return fmt.Sprintf("not sent: %s has no running session (it is %s). resume it first, then say it again.",
		t.WireName, t.Status)
}
