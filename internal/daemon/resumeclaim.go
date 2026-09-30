package daemon

import (
	"fmt"
	"log"

	"github.com/dovholuknf/atrium/internal/store"
)

// One conversation id belongs to one card, and the hooks that report it are not
// equally trustworthy about which card they mean (r-021).
//
// A hook that names itself after its directory cannot tell two cards in one
// checkout apart, and a `claude` started inside an agent's shell inherits the
// parent's ATRIUM_TASK_ID and posts as the parent. Both stored a conversation on
// the wrong card once, and the card that owned it could no longer be resumed.

// claimResume stores a conversation id on a card through the store's ranking,
// and writes what happened onto the cards' histories.
//
// `byID` is a claim bound to the card by its task id, which moves the id off a
// card that is not live. Anything else is refused against any other holder. A
// refusal is an event on BOTH cards, with the reason, because nobody reads the
// daemon log. Never an error to the hook: a session must not fail over this.
func (d *Daemon) claimResume(task *store.Task, resume, via string, byID bool) error {
	res, err := d.st.ClaimResumeID(task.ID, resume, byID, d.runnerIsLive)
	if err != nil {
		return err
	}
	if res.Stored {
		task.ResumeID = resume
	}
	for _, old := range res.Moved {
		log.Printf("[atrium] conversation %s moved from %s to %s (%s): the old card has no live session",
			resume, old.ID, task.ID, via)
		d.noteResume(old.ID, store.ResumeMoved, map[string]any{
			"resume": resume, "to": task.ID, "via": via,
			"reason": fmt.Sprintf("conversation %s moved to %q, which claimed it by its task id while this "+
				"card had no live session", resume, task.DisplayTitle()),
		})
		d.noteResume(task.ID, store.ResumeMoved, map[string]any{
			"resume": resume, "from": old.ID, "via": via,
			"reason": fmt.Sprintf("took conversation %s from %q, which had no live session",
				resume, old.DisplayTitle()),
		})
		d.publishTask(old.ID)
	}
	if h := res.Refused; h != nil {
		log.Printf("[atrium] conversation %s stays on %s, not %s (%s)", resume, h.ID, task.ID, via)
		key := task.ID + "|" + h.ID + "|" + resume
		if _, seen := d.resumeNoted.LoadOrStore(key, true); !seen {
			how := "by name"
			if byID {
				how = "by its task id"
			}
			d.noteResume(task.ID, store.ResumeRefused, map[string]any{
				"resume": resume, "holder": h.ID, "via": via,
				"reason": fmt.Sprintf("did not take conversation %s: %q holds it and is live (this claim came %s)",
					resume, h.DisplayTitle(), how),
			})
			d.noteResume(h.ID, store.ResumeRefused, map[string]any{
				"resume": resume, "claimant": task.ID, "via": via,
				"reason": fmt.Sprintf("kept conversation %s: %q claimed it %s and this card is live",
					resume, task.DisplayTitle(), how),
			})
			d.publishTask(task.ID)
			d.publishTask(h.ID)
		}
	}
	return nil
}

func (d *Daemon) noteResume(taskID, by string, payload map[string]any) {
	payload["by"] = by
	if err := d.st.AppendEvent(taskID, store.EventNotified, payload); err != nil {
		log.Printf("[atrium] record %s on %s: %v", by, taskID, err)
	}
}

// noteAnnounced records a conversation id a SessionStart announced for a card.
func (d *Daemon) noteAnnounced(taskID, resume string) {
	if resume == "" {
		return
	}
	d.announced.Store(taskID+"|"+resume, true)
	d.announced.Store(taskID+"|", true)
}

// wasAnnounced is whether a SessionStart named this id for the card.
func (d *Daemon) wasAnnounced(taskID, resume string) bool {
	_, ok := d.announced.Load(taskID + "|" + resume)
	return ok
}

// anyAnnounced is whether any SessionStart has spoken for the card since the
// daemon began.
func (d *Daemon) anyAnnounced(taskID string) bool {
	_, ok := d.announced.Load(taskID + "|")
	return ok
}

// ownsSession answers whether a SessionStart bound to a card by its task id
// came from that card's own runner, by the pid the hook found above itself.
//
// The task id is INHERITED, so a `claude -p` started in an agent's shell posts as
// the parent's card. Its pid is the nested process, never the parent's.
//
//   - A supervised card: the pid atrium started, or the first pid the runner's
//     own hook announced. The second covers a harness that is a shim starting the
//     real runner, whose pid is not the one atrium spawned, and it cannot be a
//     nested session because the parent starts first.
//   - An unsupervised card: when it has no live session, or the same process.
//   - A hook that found no pid cannot be judged and is believed.
func (d *Daemon) ownsSession(t *store.Task, pid int) bool {
	if pid <= 0 {
		return true
	}
	if run := d.sup.get(t.ID); run != nil {
		if run.pid == pid {
			run.announced.CompareAndSwap(0, int64(pid))
			return true
		}
		if run.announced.CompareAndSwap(0, int64(pid)) {
			return true
		}
		return run.announced.Load() == int64(pid)
	}
	return t.PID <= 0 || t.PID == pid || !processAlive(t.PID)
}
