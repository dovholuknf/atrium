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
//
// Defined in the store, because the HTTP layer names it too and cannot import
// this package.
const SettingSweepDead = store.SettingSweepDead

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

// Pruning on a timer, which is a different thing from sweeping and has to be.
//
// Sweeping ARCHIVES a dead card after a minute: it leaves the board and its
// whole audit log stays. Pruning DELETES, and takes the record with it. The
// board asks what wants attention now, the history asks what has ever run
// here, and archiving is what lets those be different questions.
//
// So this is the answer to a different complaint: archived rows accumulate
// forever, and nothing has ever removed one. `clear` on a column is the manual
// version and it has to be pressed.

// SettingPruneAfter is how old a finished card has to be before it is deleted
// outright, in seconds. `off`, which is the default, means never.
const SettingPruneAfter = store.SettingPruneAfter

// PruneDefaultAfter is the age offered when somebody turns it on, and not a
// default in the sense of applying to anybody who has not.
//
// Thirty days, because this deletes the only account of what a session ran and
// what it was allowed to do. The right number is a deliberate answer to "how
// far back do I ever look", and the first number that seems large is not it.
const PruneDefaultAfter = 30 * 24 * time.Hour

// pruneOld deletes finished cards past the configured age.
//
// OFF unless somebody turned it on, which is the opposite of the sweep and for
// the obvious reason: one takes a card off a screen and the other destroys
// evidence.
//
// `done` and `dead` only. Shelved is refused by the store whatever it is asked,
// and `backlog` is left out deliberately: an offered item nobody started is
// still work somebody found, and deleting it on a schedule would make the
// inbox quietly lossy.
func (d *Daemon) pruneOld() error {
	after, ok := d.pruneAfter()
	if !ok {
		return nil
	}
	n, err := d.st.Prune(after, store.PrunableStatuses...)
	if err != nil {
		return err
	}
	if n > 0 {
		log.Printf("[atrium] pruned %d card(s) older than %s, with their history", n, after)
		d.ap.Broadcast("task-removed", nil)
	}
	return nil
}

// pruneAfter reads the configured age, and whether pruning is on at all.
//
// Unset means OFF. Every other setting in here defaults to doing something;
// this one defaults to doing nothing, because the thing it does cannot be
// undone.
func (d *Daemon) pruneAfter() (time.Duration, bool) {
	v, err := d.st.Setting(SettingPruneAfter)
	if err != nil || v == "" || v == "off" {
		return 0, false
	}
	secs, err := time.ParseDuration(v + "s")
	if err != nil || secs <= 0 {
		return 0, false
	}
	// A floor, so a mistyped setting cannot turn this into "delete everything
	// that finished". An hour is far shorter than anybody would choose and far
	// longer than an accident.
	if secs < time.Hour {
		log.Printf("[atrium] %s is set to %s, which is under the one hour floor. not pruning.",
			SettingPruneAfter, secs)
		return 0, false
	}
	return secs, true
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
