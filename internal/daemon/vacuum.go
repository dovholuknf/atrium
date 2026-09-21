package daemon

import (
	"context"
	"log"
	"time"
)

// Handing back the space a pruned card leaves behind.
//
// SQLite frees pages inside the file when rows are deleted or rolled off and
// then reuses them for new writes, so the file stays at its high water mark: a
// database that pruned down to a few megabytes of live data keeps sitting at
// whatever it once grew to. The event sink is the half that stops it growing;
// this is the half that gives the freed space back to disk.
//
// A fresh database opens in incremental auto_vacuum mode (see store.Open), and
// PRAGMA incremental_vacuum hands its free pages back a bounded batch at a time
// while the room stays live. On an older database not in that mode the call is
// a no-op, so this loop is safe to run everywhere.

// VacuumEvery is how often freed pages are handed back to disk.
//
// Modest on purpose. The event sink keeps the database from growing, so this
// only has to keep pace with pruning, not with writes, and each tick reclaims a
// bounded batch (store.IncrementalVacuumPages) so it never blocks a request for
// long. Off the hot path by construction: nobody is waiting on it.
const VacuumEvery = 5 * time.Minute

// vacuumLoop reclaims free pages on a timer. Its own loop rather than the reap
// ticker: the reaper asks one question at one rate, and this is a different job
// at a slower rate.
func (d *Daemon) vacuumLoop(ctx context.Context, every time.Duration) {
	if every <= 0 {
		every = VacuumEvery
	}
	// Nothing to reclaim ever on a database that cannot be, so do not even run
	// the ticker. Said once, so an operator on an older database knows why the
	// file is not shrinking.
	if !d.st.IncrementalVacuumOn() {
		log.Printf("[atrium] database is not in incremental auto_vacuum mode, so freed pages will not be reclaimed")
		return
	}
	tick := time.NewTicker(every)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		}
		if err := d.st.IncrementalVacuum(); err != nil {
			log.Printf("[atrium] incremental vacuum: %v", err)
		}
	}
}
