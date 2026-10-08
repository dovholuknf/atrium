package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/store"
)

// `atrium ready`: a card in a context cycle saying its handoff is written.
//
// The ack the cycle waits on (newcontext.go). A command rather than a tool, like
// `atrium finish`, because it costs no tool schema in every context and works in
// every runner.
//
// IT READS THE HANDOFF INTO THE STORE. A notified event on the card carries the
// text, so the board's history shows it after the temp directory is cleaned.
//
// REFUSED, NOT IGNORED. Unlike `finish`, a refusal answers 409 and the CLI exits
// non-zero: the agent has to know the ack did not land, since nothing is cleared
// without one. No cycle waiting, and a missing or empty file, are both refused.

// ReadyRequest is a session saying its handoff is written.
type ReadyRequest struct {
	Agent  string `json:"agent"`
	TaskID string `json:"task_id,omitempty"`
}

// ReadyLine is what the CLI prints when the ack lands.
const ReadyLine = "atrium clears your context when this turn ends. End your turn now."

// WrapReadyLine is what the CLI prints when the ack of a restart wrap-up lands.
const WrapReadyLine = "atrium restarts the room when everyone is ready, and wakes you after. End your turn now."

// handoffKeep is the most of a handoff kept on the card.
const handoffKeep = 256 << 10

func (d *Daemon) handleReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSONErr(w, http.StatusMethodNotAllowed, errString("POST"))
		return
	}
	var in ReadyRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Agent) == "" && in.TaskID == "" {
		writeJSONErr(w, http.StatusBadRequest, errString("say which session this is, with agent or task_id"))
		return
	}
	task, err := d.taskFor(in.Agent, in.TaskID)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, errString("atrium has no card for this session"))
		return
	}
	out, code, err := d.ready(task)
	if err != nil {
		writeJSONErr(w, code, err)
		return
	}
	ncJSON(w, http.StatusOK, out)
}

// ready stores the card's handoff and acks its cycle.
func (d *Daemon) ready(task *store.Task) (map[string]any, int, error) {
	cur := d.nctx.get(task.ID)
	// A restart wrap-up waiting on this card takes the ack, with no file asked for: the conversation is kept.
	if (cur == nil || cur.step != NewContextLimit) && d.wrap.ack(task.ID) {
		log.Printf("[atrium] atrium ready from %s for the restart wrap-up", task.DisplayTitle())
		return map[string]any{"ok": true, "task_id": task.ID, "message": WrapReadyLine}, 0, nil
	}
	if cur == nil || cur.step != NewContextLimit || cur.acked {
		return nil, http.StatusConflict, errString("no context cycle is waiting for atrium ready on this card")
	}
	path := cur.file
	b, err := os.ReadFile(path)
	if err != nil || len(strings.TrimSpace(string(b))) == 0 {
		return nil, http.StatusConflict, fmt.Errorf("write your handoff to %s first, then run atrium ready again", path)
	}
	text, cut := string(b), false
	if len(text) > handoffKeep {
		text, cut = text[:handoffKeep], true
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	ev := map[string]any{"by": cycleBy, "handoff": text, "path": path, "bytes": len(b)}
	if cut {
		ev["cut"] = fmt.Sprintf("kept the first %d KB of %d", handoffKeep>>10, len(b)>>10)
	}
	stored := true
	if err := d.st.AppendEvent(task.ID, store.EventNotified, ev); err != nil {
		log.Printf("[atrium] could not store the handoff on %s: %v", task.ID, err)
		stored = false
	}
	// The run may have moved on between the look and here: only an ack that took counts.
	if !d.nctx.ack(task.ID) {
		return nil, http.StatusConflict, errString("no context cycle is waiting for atrium ready on this card")
	}
	log.Printf("[atrium] atrium ready from %s, %d bytes in %s", task.DisplayTitle(), len(b), path)
	d.publishTask(task.ID)
	return map[string]any{"ok": true, "task_id": task.ID, "path": path, "stored": stored, "message": ReadyLine}, 0, nil
}
