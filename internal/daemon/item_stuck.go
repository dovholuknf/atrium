package daemon

import (
	"fmt"
	"log"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// ITEMS, STAGE 2B: A BLOCKED ITEM THAT IS NOT MOVING, AND A MESSAGE THAT MAY HAVE BEEN LOST TO A CLEAR. What matters is
// whether the owner acted and whether the worker resumed, not whether a notice was "read". See
// docs/changes/items-stage2b.md and docs/rnd/atrium-items-decisions.md section 3.
//
//   - ONE ALERT PER STALL. A stall is one revision of a blocked item, and the alert is recorded on that revision
//     (`stuck_alerted`), so no tick and no second path can repeat it. The item moving is a new stall.
//   - THE ALERT IS NOT A MESSAGE TO A SESSION. It is an event on the worker's card and the card's stuck flag on the
//     board, which is where a human already looks. Nothing is typed, so an alert cannot wake the thing that is stuck.
//   - NOTHING HERE MOVES THE ITEM. It stays blocked until a turn starts, as stage 2a says.

const (
	// ItemOwnerSilentAfter is how long an owner may leave a blocked item with no answer.
	ItemOwnerSilentAfter = 15 * time.Minute
	// ItemResumeAfter is how long a worker may take to resume once the answer is delivered, and again after it is
	// typed a second time.
	ItemResumeAfter = 2 * time.Minute

	// ItemStuckSource marks the alert on the card and its escalation.
	ItemStuckSource = "item-stuck"

	ItemStuckOwnerSilent   = "owner-silent"
	ItemStuckOwnerGone     = "owner-gone"
	ItemStuckWorkerStalled = "worker-not-resumed"
)

// itemAnswerPrefix leads the answer typed to a worker, as the owner's words follow it.
const itemAnswerPrefix = "answer to your blocked question: "

// itemMissedNote leads a notice or answer handed back once after a context clear.
const itemMissedNote = "[in case you missed it: this reached you just before your context was cleared. Act on it if you " +
	"have not already.]\n"

// itemRetypeNote leads an answer typed a second time to a worker that has not resumed.
const itemRetypeNote = "[in case you missed it: this answer was sent to you and you have not picked it up.]\n"

const inCaseYouMissedIt = "[in case you missed it"

// sweepItems is the stuck check, once per liveness tick. Every blocked item not yet alerted for is read, and a failure
// on one is logged and does not stop the rest.
func (d *Daemon) sweepItems(now time.Time) {
	items, err := d.st.ItemsBlocked()
	if err != nil {
		log.Printf("[atrium] could not read the blocked items: %v", err)
		return
	}
	for _, it := range items {
		if it.UnblockAt == "" {
			d.sweepOwnerSilent(it, now)
		} else {
			d.sweepWorkerStalled(it, now)
		}
	}
}

func itemTime(s string) (time.Time, bool) {
	t, err := time.Parse(store.TimeFormat, s)
	return t, err == nil
}

// sweepOwnerSilent: a blocked item nobody has answered. A human owner has no timer, the board item is the alert. An
// owner whose session has ended is alerted for at once, and any other owner after 15 minutes.
func (d *Daemon) sweepOwnerSilent(it *store.ItemRow, now time.Time) {
	if it.OwnerKind == store.ItemOwnerHuman {
		return
	}
	task, err := d.st.Get(it.TaskID)
	if err != nil {
		return
	}
	if it.OwnerKind == store.ItemOwnerSession && d.itemOwnerGone(task) {
		d.itemAlert(task, it, ItemStuckOwnerGone)
		return
	}
	if at, ok := itemTime(it.UpdatedAt); ok && now.Sub(at) >= ItemOwnerSilentAfter {
		d.itemAlert(task, it, ItemStuckOwnerSilent)
	}
}

// itemOwnerGone: the owning session's card is gone or dead. A launcher in another room cannot be asked from here, so
// it is not gone, and only the timer applies to it. A role owner is the same, and has only the timer.
func (d *Daemon) itemOwnerGone(worker *store.Task) bool {
	if _, _, remote := d.remoteLauncher(worker); remote {
		return false
	}
	owner := d.launcherOf(worker)
	return owner == nil || owner.Status == store.StatusDead
}

// sweepWorkerStalled: the owner answered and the worker's turn has not started from it. Two minutes after the answer
// it is typed once more, and two minutes after that the human is alerted. An answer that never reached the worker is
// still queued, so typing it again would only double it: that step is spent without a message.
func (d *Daemon) sweepWorkerStalled(it *store.ItemRow, now time.Time) {
	task, err := d.st.Get(it.TaskID)
	if err != nil {
		return
	}
	if it.RetypedAt == "" {
		at, ok := itemTime(it.UnblockAt)
		if !ok || now.Sub(at) < ItemResumeAfter {
			return
		}
		first, err := d.st.ItemRetyped(it.ID, it.Revision, now.UTC().Format(store.TimeFormat))
		if err != nil || !first {
			return
		}
		if it.UnblockDelivered {
			text := itemRetypeNote + itemAnswerPrefix + it.UnblockText
			if _, _, err := d.deliverPeerCaused(task, store.ReservedHandle, text, "", "",
				d.waitsForTurn(task.ID, WhenImmediate)); err != nil {
				log.Printf("[atrium] could not type item %s's answer again: %v", it.ID, err)
			}
		}
		return
	}
	if at, ok := itemTime(it.RetypedAt); ok && now.Sub(at) >= ItemResumeAfter {
		d.itemAlert(task, it, ItemStuckWorkerStalled)
	}
}

// itemAlert records the one alert for this stall. Whoever records it first writes it.
func (d *Daemon) itemAlert(task *store.Task, it *store.ItemRow, why string) {
	first, err := d.st.ItemStuck(it.ID, it.Revision, why)
	if err != nil || !first {
		return
	}
	text := itemStuckText(it, why)
	if err := d.st.AppendEvent(task.ID, store.EventNotified, map[string]any{
		"source": ItemStuckSource, "item_id": it.ID, "why": why, "text": text,
	}); err != nil {
		log.Printf("[atrium] could not record the stuck alert for item %s: %v", it.ID, err)
	}
	log.Printf("[atrium] %s", text)
	d.Publish(task.ID)
}

func itemStuckText(it *store.ItemRow, why string) string {
	switch why {
	case ItemStuckOwnerGone:
		return "item " + it.ID + " is STUCK: it is blocked and its owner has no session to answer it"
	case ItemStuckWorkerStalled:
		return "item " + it.ID + " is STUCK: its owner answered and the worker has not picked the answer up"
	}
	return "item " + it.ID + " is STUCK: it has been blocked for " + fmt.Sprint(int(ItemOwnerSilentAfter/time.Minute)) +
		" minutes with no answer from its owner"
}

// itemStuckEscalation is the card's stuck flag for an alerted item, so the board shows it where it already looks.
func (d *Daemon) itemStuckEscalation(t *store.Task) *Escalation {
	if !isItem(t) {
		return nil
	}
	it, err := d.st.ItemOfTask(t.ID)
	if err != nil || it == nil || it.State != store.ItemBlocked || it.StuckAlerted == "" {
		return nil
	}
	since, ok := itemTime(it.UpdatedAt)
	if !ok {
		since = time.Now()
	}
	return &Escalation{
		Source: ItemStuckSource, Since: since, Count: 1, Minutes: int(time.Since(since) / time.Minute),
		Text: itemStuckText(it, it.StuckAlerted),
	}
}

// ── a clear ─────────────────────────────────────────────────────────────────

// itemMissed is what a context clear hands back of an item's notice or answer. The says an item sent, still pending,
// come back ONCE with "in case you missed it", and an item that has moved on, or one already handed back, is dropped.
// Everything else passes through for the ordinary carry. The alert after that is the stall timers above.
func (d *Daemon) itemMissed(taskID string, carried []deliveredSay) []deliveredSay {
	var out []deliveredSay
	for _, s := range carried {
		if s.from != store.ReservedHandle {
			out = append(out, s)
			continue
		}
		body := strings.TrimPrefix(s.text, itemRetypeNote)
		switch {
		case strings.HasPrefix(body, itemAnswerPrefix):
			if d.itemAnswerPending(taskID) {
				s.text = itemMissedNote + body
				out = append(out, s)
			}
		case strings.HasPrefix(body, "item it-"):
			if d.itemNoticePending(taskID, body) {
				s.text = itemMissedNote + body
				out = append(out, s)
			}
		default:
			out = append(out, s)
		}
	}
	return out
}

// itemAnswerPending: the worker's item is still blocked on the answer, and this is the first time it is handed back.
func (d *Daemon) itemAnswerPending(taskID string) bool {
	it, err := d.st.ItemOfTask(taskID)
	if err != nil || it == nil || it.State != store.ItemBlocked || it.UnblockText == "" {
		return false
	}
	first, err := d.st.ItemMissed(it.ID, it.Revision, true)
	return err == nil && first
}

// itemNoticePending: the owner's notice is about the item as it still is. A blocked notice is pending while nobody has
// answered, a finished one while the item is finished. A worker's own card is never an owner.
func (d *Daemon) itemNoticePending(taskID, body string) bool {
	id, _, _ := strings.Cut(strings.TrimPrefix(body, "item "), " ")
	it, err := d.st.ItemGet(id)
	if err != nil || it.TaskID == taskID {
		return false
	}
	switch {
	case strings.Contains(body, ") blocked: "):
		if it.State != store.ItemBlocked || it.UnblockAt != "" {
			return false
		}
	case strings.Contains(body, ") finished"):
		if it.State != store.ItemFinished {
			return false
		}
	default:
		return false
	}
	first, err := d.st.ItemMissed(it.ID, it.Revision, false)
	return err == nil && first
}
