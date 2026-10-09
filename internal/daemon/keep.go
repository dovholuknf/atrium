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

// How a woken card is given what it holds. Vars so a test can shorten them.
var (
	// keptTick is how often a woken card is looked at until its runner can take input.
	keptTick = 2 * time.Second
	// keptSettle is how long after the runner's SessionStart hook its input box can be trusted. See wakeSettle.
	keptSettle = wakeSettle
	// keptGiveUp is how long a woken card may be unready before its launcher is told.
	keptGiveUp = 3 * time.Minute
)

// injectKept hands a card that is waking what it holds, as ONE submitted turn, once its runner can take input. A
// resumed session sits at its prompt and makes no tool call and ends no turn, so the hooks alone would leave the words
// there. Typed straight away, they land on the startup screen and are lost, and a row marked delivered then keeps the
// card from being thought of as holding anything: it sat idle for two hours until the idle park ended it
// (r-wake-say-resumes-but-not-delivered). So nothing is typed before the runner is ready, every pending row goes in
// the same turn, the rows are marked delivered only after the bytes are written, and a card that is not ready in
// keptGiveUp has its launcher told. The deferred per-message retry stands aside meanwhile: see deferPeerInjection.
func (d *Daemon) injectKept(taskID string) {
	if _, running := d.kept.LoadOrStore(taskID, true); running {
		return
	}
	go func() {
		defer d.kept.Delete(taskID)
		d.deliverKept(taskID)
	}()
}

// keptRunnerReady reports whether the runner now on a card can take input: its SessionStart hook has fired and settled,
// or a runner with no hooks has been up a minute. The after-restart wake asks the same question.
func (d *Daemon) keptRunnerReady(taskID string, run *runner, now time.Time) bool {
	if d.wake != nil {
		if at, ok := d.wake.sessionStarted(taskID); ok && !at.Before(run.started) {
			return now.Sub(at) >= keptSettle
		}
	}
	return now.Sub(run.started) >= wakeNoHook
}

// deliverKept waits for the card's runner, then types every pending message as one turn.
func (d *Daemon) deliverKept(taskID string) {
	deadline := time.Now().Add(keptGiveUp)
	for {
		msgs, err := d.st.PendingMessages(taskID)
		if err != nil {
			log.Printf("[atrium] could not read what is kept for %s: %v", taskID, err)
			return
		}
		if len(msgs) == 0 {
			return
		}
		now := time.Now()
		if run := d.sup.get(taskID); run != nil && d.keptRunnerReady(taskID, run, now) &&
			!d.act.dialogOpen(taskID) && !d.act.midTurn(taskID) && !d.holdingMessages(taskID) && !d.deployHeld(taskID) {
			wrote, err := d.typeLabelledThroughGate(run, taskID, "", messageBanner(msgs, false))
			if err != nil {
				log.Printf("[atrium] could not type what is kept into %s: %v", taskID, err)
			}
			if wrote {
				ids := make([]string, 0, len(msgs))
				for _, m := range msgs {
					ids = append(ids, m.ID)
				}
				if err := d.st.MarkDelivered(taskID, "terminal", ids); err != nil {
					log.Printf("[atrium] typed what is kept into %s but could not mark it delivered: %v", taskID, err)
				}
				for _, m := range msgs {
					d.notePeerTyped(taskID, m.FromPeer, m.Text, "", "typed and sent when the resumed session was ready")
				}
				if d.pending != nil {
					d.pending.deliveredElsewhere(taskID, ids)
				}
				d.publishTask(taskID)
				return
			}
		}
		if now.After(deadline) {
			d.keptUndelivered(taskID, len(msgs))
			return
		}
		time.Sleep(keptTick)
	}
}

// keptUndelivered tells the launcher that a woken card never took what it holds. The rows stay pending, so the hooks of
// the session still carry them if it makes a call, but nobody is left believing they were read.
func (d *Daemon) keptUndelivered(taskID string, n int) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return
	}
	log.Printf("[atrium] %s was woken but its runner never took %d held message(s)", t.DisplayTitle(), n)
	d.notifyLauncher(t, "wake-undelivered", fmt.Sprintf("%s:%d", taskID, time.Now().Unix()),
		fmt.Sprintf("%s was woken but its session did not take the %d message(s) held for it, so it is idle and has not "+
			"read them. They are still on its card. Resume it and say again, or end it.", t.WireName, n))
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
