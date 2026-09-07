package daemon

import (
	"context"
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The reaper answers "is this session still there" without spending a token.
// A card carries the runner's process id, and whether that process exists is a
// question the operating system answers for free. Nothing is sent to the
// runner, nothing wakes its model, and a session that died in the night is
// marked dead rather than sitting in running forever.
//
// A card with no known pid cannot be checked that way, so it gets the other
// test: how long since anything at all was heard from it. A session that has
// said nothing for hours is not running in any useful sense, and a `running`
// column full of those is what makes the board untrustworthy.
//
// Either way the card only moves to dead. Any later hook event revives it, so
// being wrong costs one status change rather than losing work.

// ReapEvery is how often liveness is checked. The check is a syscall per card,
// so this can be frequent without costing anything meaningful.
const ReapEvery = 20 * time.Second

// QuietAfter is how long a card with no known process id may go without a word
// before it is treated as gone.
//
// It was three hours, which is far too generous for what it is measuring. A
// card with no pid never reported one, so its session hook never ran, which
// makes it the kind of session atrium knows least about rather than the kind
// to give the most benefit of the doubt to. Meanwhile the board showed it as
// `running` for the rest of the afternoon, in the column that is supposed to
// mean something is working.
//
// The cost of being early is a card that flips back to running the moment the
// session says anything, because any activity revives it. The cost of being
// late is a running column that cannot be trusted. Fifteen minutes: a session
// silent that long with nothing to check is not something to keep claiming is
// working, and if it was, it says so and comes straight back.
const QuietAfter = 15 * time.Minute

// reviveOwnedDead is the reaper run the other way: a card filed dead with a
// process provably still on it.
//
// The reaper is the one thing here that asks the operating system rather than
// waiting to be told, and the disagreement worth eliminating is a card whose
// status says one thing and whose process says the other. It has to run in
// both directions or it only half works. A launch onto a dead card moves it
// (see `statusAfterLaunchOnto`), and this catches every other path that does
// not, including ones written after this one.
//
// ONLY RUNNERS ATRIUM OWNS, and asked of the supervisor rather than of the
// card's pid. A dead card's pid is the pid it had when it died, and the
// operating system recycles pids, so `processAlive` on an old dead card can be
// true about a process that has nothing to do with it. The supervisor holds an
// entry only while the process it started is running and drops it in
// `awaitExit`, so it cannot be wrong in that direction. The honest limit is
// that a window-mode launch is owned by its terminal and never appears here.
//
// It is asked of the supervisor rather than of the board for a second reason:
// `List` does not return archived cards, and a dead card is archived off the
// board within a minute. The card being invisible is the symptom, so a check
// that could not see it would be exactly no use.
//
// `needs-input` with `started`, matching the launch path and session.go: there
// is a process and nothing has been heard from it. `running` would put a card
// in the one column that means work is happening on the strength of a pid.
// Moving the status also clears `archived_at`, so a card the sweep already
// took away comes back onto the board.
func (d *Daemon) reviveOwnedDead() error {
	for _, r := range d.sup.all() {
		t, err := d.st.Get(r.taskID)
		if err != nil {
			// A card deleted out from under its runner is not this job's
			// problem to report. Terminating it is.
			continue
		}
		if t.Status != store.StatusDead {
			continue
		}
		if err := d.st.AppendEvent(t.ID, store.EventLaunched, map[string]any{
			"by": "reaper", "detected": "filed dead while atrium still owns its runner",
			"pid": t.PID,
		}); err != nil {
			return err
		}
		if err := d.st.SetStatusBecause(t.ID, store.StatusNeedsInput, store.WaitingStarted); err != nil {
			return err
		}
		log.Printf("[atrium] %s was filed dead with a live runner on it, back on the board",
			t.DisplayTitle())
		d.publishTask(t.ID)
	}
	return nil
}

func (d *Daemon) reapOnce() error {
	// Both directions, and this one first. A card that is about to be revived
	// must not be swept off the board in the same tick for being dead.
	if err := d.reviveOwnedDead(); err != nil {
		return err
	}
	tasks, err := d.st.List(store.StatusRunning, store.StatusNeedsInput, store.StatusNeedsPermission)
	if err != nil {
		return err
	}
	for _, t := range tasks {
		// No pid to ask about, so fall back to silence. A card waiting on a
		// human is exempt: it is quiet because nobody has answered it, and
		// marking it dead would discard the question.
		if t.PID <= 0 {
			if t.Status != store.StatusRunning {
				continue
			}
			if time.Since(t.LastActivityAt) < QuietAfter {
				continue
			}
			if err := d.st.AppendEvent(t.ID, store.EventExited, map[string]any{
				"by": "reaper", "detected": "no contact and no process id to check",
				"quiet_for": time.Since(t.LastActivityAt).Round(time.Minute).String(),
			}); err != nil {
				return err
			}
			if err := d.st.SetStatus(t.ID, store.StatusDead); err != nil {
				return err
			}
			d.act.forget(t.ID)
			log.Printf("[atrium] %s assumed gone: silent for %s and no pid to check",
				t.DisplayTitle(), time.Since(t.LastActivityAt).Round(time.Minute))
			d.publishTask(t.ID)
			continue
		}
		if processAlive(t.PID) {
			continue
		}
		if err := d.st.AppendEvent(t.ID, store.EventExited, map[string]any{
			"pid": t.PID, "by": "reaper", "detected": "process is gone",
		}); err != nil {
			return err
		}
		if err := d.st.SetStatus(t.ID, store.StatusDead); err != nil {
			return err
		}
		// Nothing a gone process was doing is still true.
		d.act.forget(t.ID)
		log.Printf("[atrium] %s is dead: pid %d is gone", t.DisplayTitle(), t.PID)
		d.publishTask(t.ID)
	}
	return nil
}

func (d *Daemon) reap(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = ReapEvery
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	var lastErr string
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		// Questions asked for sessions that have gone. Same tick, because it
		// is the same job: deciding what is still there. Its own error is
		// logged rather than skipping the liveness check, since the two are
		// independent and one failing is no reason to stop the other.
		if err := d.reapOrphans(); err != nil {
			log.Printf("[atrium] orphan check: %v", err)
		}
		// WHICH CARDS ARE ALIVE IS SETTLED FIRST, before anything acts on the
		// answer. The sweep below archives dead cards, and a card the liveness
		// check was about to revive must not be taken off the board in the
		// same tick for a status that is one call away from being corrected.
		// A tick that could not answer the question sweeps nothing.
		if err := d.reapOnce(); err != nil {
			if msg := err.Error(); msg != lastErr {
				log.Printf("[atrium] liveness check: %v", err)
				lastErr = msg
			}
			continue
		}
		lastErr = ""
		// Dead cards go on their own. Same ticker as the reaper, because it is
		// the same question at the same rate and a second ticker is a second
		// thing to get wrong at shutdown.
		if err := d.sweepDead(); err != nil {
			log.Printf("[atrium] sweeping dead cards: %v", err)
		}
		// And deleting the ones old enough that nobody is going to read them.
		// Off unless configured, because this one destroys the record rather
		// than moving it off a screen.
		if err := d.pruneOld(); err != nil {
			log.Printf("[atrium] pruning old cards: %v", err)
		}
		// And settled items from the dispatch queue, which is the same job for
		// a different table. Only settled ones: an item nobody has collected is
		// a promise, and it ages out through its lease rather than through
		// here. See `internal/store/dispatch.go`.
		if n, err := d.st.SweepDispatch(DispatchKeepSettled); err != nil {
			log.Printf("[atrium] sweeping the dispatch queue: %v", err)
		} else if n > 0 {
			log.Printf("[atrium] cleared %d settled dispatch item(s)", n)
			d.ap.Broadcast("dispatch", nil)
		}
	}
}
