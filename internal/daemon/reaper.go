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

func (d *Daemon) reapOnce() error {
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
		if err := d.reapOnce(); err != nil {
			if msg := err.Error(); msg != lastErr {
				log.Printf("[atrium] liveness check: %v", err)
				lastErr = msg
			}
			continue
		}
		lastErr = ""
	}
}
