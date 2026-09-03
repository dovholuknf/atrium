package daemon

import (
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Taking dead cards off the board on their own.
//
// A dead card is a session that has ended, and atrium is sure: either the
// operating system said its pid is gone, or nothing was heard for the quiet
// window with no pid to check. What it keeps is the history, and the history
// is already kept somewhere better than a column: the card's own audit log,
// which the sweep deletes along with it.
//
// So the argument for keeping them on screen is thin. A finished column that
// fills up all day is a column nobody reads, and a `clear` button that has to
// be pressed is a chore invented by the tool.
//
// Deliberately NOT applied to `done`. Done is a human saying "this is
// finished", and a board that deleted what you filed the moment you filed it
// would make filing pointless. Only `dead` sweeps, because only `dead` is
// atrium's own conclusion rather than yours.

// SweepDeadAfter is how long a dead card stays before it is deleted.
//
// Short, because there is nothing to come back for: a session that revives
// says so and gets its card back, and one that does not was over.
//
// Not zero. A card that vanished the instant it died would take the reason
// with it, and the reason is the one thing worth reading on a dead card: a
// runner that failed to start puts its last output there. A minute is long
// enough to see it and short enough not to accumulate.
var SweepDeadAfter = time.Minute

// SettingSweepDead turns it off. Stored as the number of seconds, so the
// setting is also the answer to "how long", and `off` is the one value that
// means never.
const SettingSweepDead = "sweep_dead_after"

// sweepDead deletes dead cards that have been dead long enough.
//
// Runs on the reap ticker rather than a timer of its own: it is the same
// question asked at the same rate, and a second ticker is a second thing to
// get wrong at shutdown.
func (d *Daemon) sweepDead() error {
	after, ok := d.sweepAfter()
	if !ok {
		return nil
	}
	// Archived, not deleted. The card and its audit log are the only account
	// of what that session ran and what it was allowed to do, and a timer that
	// discarded that a minute after a session ended would make the question
	// "what have I had running this week" unanswerable. `clear` still deletes,
	// because that is a human saying so.
	//
	// Dead only. `done` is a human saying the work is finished, and a board
	// that filed away what you just filed would make filing pointless. Only
	// dead sweeps, because only dead is atrium's own conclusion.
	swept, err := d.st.Archive(after, store.StatusDead)
	if err != nil {
		return err
	}
	if swept > 0 {
		log.Printf("[atrium] archived %d dead card(s)", swept)
		d.ap.Broadcast("task-removed", nil)
	}
	return nil
}

// sweepAfter reads the configured age, and whether sweeping is on at all.
//
// A read failure answers "off". This deletes things, and the safe answer to a
// question it could not read is to do nothing.
func (d *Daemon) sweepAfter() (time.Duration, bool) {
	v, err := d.st.Setting(SettingSweepDead)
	if err != nil {
		return 0, false
	}
	switch v {
	case "":
		// Never configured, so the default applies.
		return SweepDeadAfter, true
	case "off":
		return 0, false
	}
	secs, err := time.ParseDuration(v + "s")
	if err != nil || secs <= 0 {
		return 0, false
	}
	return secs, true
}
