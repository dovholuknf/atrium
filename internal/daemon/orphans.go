package daemon

import (
	"log"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A question asked for a session that is no longer there.
//
// The reaper answers "is this session alive" two ways: ask the operating
// system about its pid, or fall back to how long it has been silent. Neither
// reaches a card waiting on a human with no pid recorded. Silence is not a
// signal there, because waiting is supposed to be silent, and that is why a
// card in needs-permission is exempt from the quiet check: marking it dead
// would discard the question.
//
// So it sits there. The permission queue keeps offering a request nobody can
// answer, and answering it writes a decision into a channel that closed when
// the agent went away. That is the hole, and it is narrow: it needs a session
// that died between asking and being answered, with no pid on the card.
//
// The hub can settle it. Only this process knows which pending requests still
// have somebody parked on them, because the reply channel lives here and dies
// with the connection. A request the store calls pending and the hub has never
// heard of is an orphan.

// OrphanGrace is how long a pending request may go unmatched before it is
// treated as orphaned.
//
// Not zero, because the two facts are read at different moments. A request
// recorded a millisecond ago may not have reached the hub's map yet, and a
// daemon that restarted has a store full of pending requests and an empty map
// until every agent reconnects on its own backoff, which runs to a minute.
// A variable rather than a constant so a test can be on the other side of it
// without waiting three minutes. Nothing in production writes to it.
var OrphanGrace = 3 * time.Minute

// orphanReason is handed to nothing, since there is nobody to hand it to. It
// is recorded so the audit log says why the card moved.
const orphanReason = "the session that asked this went away before it was answered"

// reapOrphans closes out requests whose agent has gone.
//
// The request is answered with a block so the queue stops offering it, and the
// card moves to dead only when nothing else is holding it. The decision is
// recorded like any other, because a request leaving the queue without a
// reason in the log is the thing the log exists to prevent.
func (d *Daemon) reapOrphans() error {
	pending, err := d.st.PendingPermissions()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	live := d.hb.LiveStoreIDs()

	byTask := map[string]int{}
	for _, p := range pending {
		if live[p.ID] {
			// Somebody is parked on it. This is the ordinary case.
			byTask[p.TaskID]++
			continue
		}
		if time.Since(p.RequestedAt) < OrphanGrace {
			// Too soon to tell the two facts apart.
			byTask[p.TaskID]++
			continue
		}
		if _, err := d.st.DecidePermissionBy(p.ID, "block", orphanReason, "orphaned"); err != nil {
			return err
		}
		log.Printf("[atrium] closed an orphaned request on %s: %s", p.TaskID, p.Tool)
	}

	// A card whose only reason to be waiting has just gone gets moved. Left in
	// needs-permission it would claim to be asking something.
	for _, p := range pending {
		if byTask[p.TaskID] > 0 {
			continue
		}
		t, err := d.st.Get(p.TaskID)
		if err != nil {
			continue
		}
		if t.Status != store.StatusNeedsPermission {
			continue
		}
		// A pid means the reaper can answer this properly, and its answer is
		// a fact where this one is an inference.
		if t.PID > 0 {
			continue
		}
		if err := d.st.AppendEvent(t.ID, store.EventExited, map[string]any{
			"by": "orphan-reaper", "detected": "asked a question and then went away",
		}); err != nil {
			return err
		}
		if err := d.st.SetStatus(t.ID, store.StatusDead); err != nil {
			return err
		}
		d.act.forget(t.ID)
		log.Printf("[atrium] %s went away while waiting to be answered", t.DisplayTitle())
		d.publishTask(t.ID)
		byTask[p.TaskID]++
	}
	return nil
}
