package daemon

import (
	"encoding/json"
	"log"
	"net/http"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
	"github.com/dovholuknf/atrium/internal/small"
	"github.com/dovholuknf/atrium/internal/store"
)

// "Force": a say that types NOW, past the typing gate and the turn rule.
//
// After a room restart the gate is "not sure what is on the line", because the model lost what was typed, and it holds
// every say until Enter or control-c gives it something to follow. The board's let-through refuses too, since it will
// only retype a line it can read exactly. Both are right to be careful, and both leave a session nobody can reach
// without a hand written websocket client. `force: true` is the way out, said on purpose by whoever sends.
//
// WHO MAY. The operator (no `from`, and the board's Type anyway) skips the whole typing gate and the turn rule. A peer
// is not that human: only the target's launcher may force, and only past a line the gate cannot READ. A part written
// line atrium knows of, or a key pressed in the last peerGateIdle, refuses it with the reason and nothing is queued or
// typed, because the Enter would submit the operator's half typed words with the message glued on. Any other peer is
// refused.
//
// WHAT IT OVERRIDES: the typing gate and the turn rule (immediate types mid-turn anyway, `when: done` and a runner that
// only queues mid-turn included).
//
// WHAT IT NEVER OVERRIDES, and refuses with the reason rather than queueing, because the sender asked for now:
//   - a dialog on the runner's screen, whose answer the Enter would be;
//   - a new-context cycle or a room deploy hold, which own the terminal.
//
// A card with no terminal of ours, a parked or gone card, still goes the ordinary way. Force changes how a line is
// judged, not whether there is a terminal to type into.
//
// It is recorded on the card's history under its own name: the prompted event says how, `typed anyway`, and carries
// `forced: true`.

const forcedHow = "typed anyway, past the typing gate"

// handleTypeAnyway is the board chip's "Type anyway": every message held for the line is typed and submitted now,
// oldest first, past the gate. Unlike let-through it does not touch the line, so it is the answer when atrium cannot
// read the line and let-through refuses. A dialog or a new-context cycle refuses it, as for a forced say.
func (d *Daemon) handleTypeAnyway(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	run := d.sup.Get(taskID)
	if d.pending == nil || run == nil {
		writeJSONCode(w, http.StatusNotFound, errBody("no supervised terminal on this card"))
		return
	}
	d.pending.mu.Lock()
	var entries []pendingMsg
	if ht := d.pending.by[taskID]; ht != nil {
		entries = append(entries, ht.entries...)
	}
	d.pending.mu.Unlock()
	if len(entries) == 0 {
		writeJSONCode(w, http.StatusConflict, errBody("nothing is waiting"))
		return
	}
	switch {
	case d.holdingMessages(taskID):
		writeJSONCode(w, http.StatusConflict, errBody("a new-context cycle owns that terminal, so nothing was typed"))
		return
	case d.act.dialogOpen(taskID):
		writeJSONCode(w, http.StatusConflict, errBody("a dialog is on that terminal, and the Enter would answer it"))
		return
	}
	delivered := 0
	for _, e := range entries {
		wrote, err := run.InjectForced(e.banner, e.body, nil)
		if err != nil {
			small.WriteJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		if !wrote {
			break
		}
		if err := d.markDelivered(taskID, "terminal", []string{e.msgID}); err != nil {
			log.Printf("[atrium] typed a held message into %s anyway but could not mark it delivered: %v", taskID, err)
		}
		d.notePeerTyped(taskID, e.from, e.text, e.kind, forcedHow)
		delivered++
	}
	d.publishTask(taskID)
	writeJSONCode(w, http.StatusOK, map[string]any{"delivered": delivered})
}

// forceSay answers true when it has written the response, a delivery or a refusal, and false when the ordinary path
// should carry on (no terminal of ours to type into).
func (d *Daemon) forceSay(w http.ResponseWriter, taskID, from, text, kind, rawKind string, reply, holding, waitTurn bool,
	rec *store.Say, target *store.Task) bool {
	run := d.sup.Get(taskID)
	if run == nil {
		return false
	}
	switch {
	case d.holdingMessages(taskID):
		writeJSONCode(w, http.StatusConflict, errBody("force refused: a new-context cycle owns that terminal. "+newContextHoldNote))
		return true
	case holding:
		writeJSONCode(w, http.StatusConflict, errBody("force refused: "+d.deployHoldNote()))
		return true
	case d.act.dialogOpen(taskID):
		writeJSONCode(w, http.StatusConflict, errBody("force refused: a dialog is on that terminal, and the Enter would answer it"))
		return true
	}
	banner := ""
	var ok func() bool
	if from != "" {
		// A PEER is not that human. Only the target's launcher may force, and only past a line atrium cannot read.
		if !d.isLauncherOf(from, target) {
			writeJSONCode(w, http.StatusForbidden, errBody("force refused: only the operator or the launcher of that session may force a say"))
			return true
		}
		if why := run.ForceRefusal(); why != "" {
			writeJSONCode(w, http.StatusConflict, errBody("force refused: "+why+". force only types past a line atrium cannot read"))
			return true
		}
		banner, ok = peerBanner(from), run.PeerForceOK
	}
	payload := text
	if d.bracketedPasteFor(taskID, false) {
		payload = screen.BracketedPaste(text)
	}
	// Read before the write: whether it lands in a queued box depends on the state the line is typed into.
	inBox := d.typedWouldQueue(taskID)
	wrote, err := run.InjectForced(banner, payload, ok)
	if err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return true
	}
	if !wrote {
		writeJSONCode(w, http.StatusConflict, errBody("force refused: the line changed under it, so nothing was typed"))
		return true
	}
	ev := map[string]any{"text": text, "via": "terminal", "how": forcedHow, "forced": true}
	if from != "" {
		ev["from_peer"] = from
	}
	if kind != "" {
		ev["kind"] = kind
	}
	if err := d.st.AppendEvent(taskID, store.EventPrompted, ev); err != nil {
		small.WriteJSONErr(w, http.StatusInternalServerError, err)
		return true
	}
	if from != "" {
		d.noteDelivery(taskID, from, store.DeliveryKind(from, ""))
	}
	d.askAnswered(taskID, "the operator")
	d.noteInContext(taskID, from, text, kind)
	if target != nil {
		d.peerSaid(from, target, text, owedKind(rawKind, reply))
	}
	if from == "" {
		d.seenAnswered(taskID, store.SeenMessage)
	}
	d.publishTask(taskID)
	out := map[string]any{"delivered": "terminal", "forced": true, "when": whenWord(waitTurn)}
	if inBox {
		// Typed past the turn rule into a runner that queues it: in its box, not read. See typedQueued.
		out["delivered"], out["state"], out["read"] = TypedQueued, TypedQueued, false
		out["note"] = typedQueuedNote
	}
	if rec != nil {
		rec.State, rec.Channel = store.SayDelivered, store.SayViaTerminal
		if inBox {
			// In the box, not read: `typed` until the transcript shows it drained. See typedproof.go.
			rec.State = store.SayTyped
		}
		if id := d.recordSay(*rec, text); id != "" {
			out["say"] = id
		}
		out["via"] = rec.Via
		d.saySettled(from, target)
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
	return true
}
