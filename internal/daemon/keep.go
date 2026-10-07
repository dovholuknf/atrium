package daemon

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A say that nobody can read yet is KEPT, not refused.
//
// It used to be refused with nothing stored when the card was parked and the sender did not ask to wake it, and when
// the card was done or dead. The sender was told, but the words were gone, and a sender that is an agent mid-task does
// not always read the refusal. Now the message is a row in `message` before the sender is answered, it shows on the card
// as undelivered, and it is typed in or carried by the hooks the next time the card runs. Nothing expires. See
// docs/changes/r-nothing-gets-lost.md.

// keptNote is what the sender of a kept say is told.
func keptNote(t *store.Task, why string) string {
	return fmt.Sprintf("kept on %s: %s. it is stored on the card and delivered the next time it runs, "+
		"and stays there until it is read.", t.WireName, why)
}

// resumable reports whether a card with no session has a conversation to resume.
func resumable(t *store.Task) bool {
	return t != nil && t.ResumeID != "" && t.Runner != "" && t.Worktree != ""
}

// keepOnCard stores a say for a card that cannot read it now, records it, and answers the sender. The message is a
// durable row before the answer is written.
func (d *Daemon) keepOnCard(w http.ResponseWriter, from string, target *store.Task, verb, text, when, kind string,
	reply bool, why string) {
	m, err := d.st.QueuePeerKind(target.ID, text, from, promptKind(kind, reply), false)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	rec := sayRecordFor(from, target, sayTrace{}, false, verb, when, reply)
	rec.State, rec.MessageID = store.SayQueued, m.ID
	out := map[string]any{
		"queued": true, "typed": false, "delivered": "queued", "reachable": "kept", "id": m.ID,
		"to": target.WireName, "note": keptNote(target, why), "when": when,
	}
	if id := d.recordSay(rec, text); id != "" {
		out["say"] = id
	}
	d.peerSaid(from, target, text, owedKind(kind, reply))
	d.publishTask(target.ID)
	log.Printf("[atrium] kept %s's say to %s (%s)", from, target.WireName, why)
	writeJSONCode(w, http.StatusOK, out)
}

// injectKept types what was kept for a card into its session, now that one is starting. A resumed session sits at
// its prompt and makes no tool call and ends no turn, so the hooks alone would leave the words there. It goes through
// the operator's retry, which waits for a free line. The row stays undelivered until the session has it.
func (d *Daemon) injectKept(taskID string) {
	msgs, err := d.st.PendingMessages(taskID)
	if err != nil {
		log.Printf("[atrium] could not read what is kept for %s: %v", taskID, err)
		return
	}
	for _, m := range msgs {
		d.deferPeerInjection(taskID, m.ID, "", messageBanner([]*store.Message{m}, false), "", false)
	}
}

// wakeGone resumes a done or dead card that has a conversation, the way unpark does for a parked one. The card is
// not parked, so there is no flag to clear: the launch is the whole of it.
func (d *Daemon) wakeGone(taskID, via string) error {
	t, err := d.st.Get(taskID)
	if err != nil {
		return err
	}
	unlock := d.launching.lock(launchKeys(taskID, t.ResumeID)...)
	defer unlock()
	t, err = d.st.Get(taskID)
	if err != nil {
		return err
	}
	if !d.sessionGone(t) {
		return nil
	}
	if !resumable(t) {
		return fmt.Errorf("%s cannot be resumed: nothing records its conversation", t.DisplayTitle())
	}
	launch := d.launchLocked
	if d.wakeLaunch != nil {
		launch = d.wakeLaunch
	}
	if _, err := launch(d.reopenRequest(t)); err != nil {
		return fmt.Errorf("could not resume %s: %w", t.DisplayTitle(), err)
	}
	d.startedAt.Store(taskID, time.Now())
	d.queueHandoffWake(taskID)
	d.injectKept(taskID)
	log.Printf("[atrium] %s had finished and is resumed (%s)", t.DisplayTitle(), via)
	d.publishTask(taskID)
	return nil
}
