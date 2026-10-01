package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Switching a live card's model: `POST /v1/tasks/{id}/model`.
//
// Claude Code's `/model <id>` changes the model of a running session, and atrium
// owns the terminal, so atrium types it. No new context and no relaunch.
//
// TYPED THROUGH THE SAME GATE AS A SAY. An empty line, a quiet keyboard, no dialog
// on screen, and the turn rule an immediate say follows. A closed gate is not an
// error: the switch waits and the answer says so.
//
// NOT THE MESSAGE QUEUE. A queued message reaches the model as text through a hook,
// and `/model x` delivered that way is a sentence, not a command. So the wait is
// its own small retry here, one per card, newest request wins.
//
// THE STORE IS WRITTEN AT REQUEST TIME, typed or not. The card's model is what a
// relaunch or a resume starts on (`launch.go`, `park.go`), so a switch that is still
// waiting, or that the runner left before typing, must survive into the next start.
//
// TELEMETRY DOES NOT FIGHT IT. A statusline posts a display name ("Opus 5") into
// the activity tracker, which is a fact about now and never written to the card.
// `task.Model` is what was asked for. Neither overwrites the other.

// modelSwitchBy is the `by` of the card event a switch writes. The event kinds are
// a closed set and a new one is a table rebuild, so it rides `notified`.
const modelSwitchBy = "model-switch"

// modelRetryEvery is how often a waiting switch tries the gate again, and
// modelWaitMax is how long it keeps trying. Variables so a test need not wait.
var (
	modelRetryEvery = 2 * time.Second
	modelWaitMax    = 30 * time.Minute
)

// modelAliases are the names Claude Code takes for a model family.
var modelAliases = map[string]bool{"sonnet": true, "opus": true, "haiku": true, "fable": true}

// modelWait is a switch that has not been typed yet.
type modelWait struct {
	model, by string
}

// validModel normalises and checks what `/model` will be given. An alias, or an id
// shaped like `claude-...`. Nothing else is allowed through: the value is typed into
// a terminal, so it carries no whitespace, quote or control character.
func validModel(raw string) (string, bool) {
	m := strings.TrimSpace(raw)
	if m == "" {
		return "", false
	}
	if low := strings.ToLower(m); modelAliases[low] {
		return low, true
	}
	if !strings.HasPrefix(strings.ToLower(m), "claude-") || len(m) > 80 {
		return "", false
	}
	for _, r := range m {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
		case r == '-', r == '.', r == '_', r == '[', r == ']':
		default:
			return "", false
		}
	}
	return m, true
}

// handleModel is `POST /v1/tasks/{id}/model`.
func (d *Daemon) handleModel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Model string `json:"model"`
		// From is who asked, for the card's record. Empty is the operator.
		From string `json:"from"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	model, ok := validModel(body.Model)
	if !ok {
		writeJSONErr(w, http.StatusBadRequest, fmt.Errorf("a model is sonnet, opus, haiku, fable or an id "+
			"like claude-opus-4-7, got %q", body.Model))
		return
	}
	taskID := r.PathValue("id")
	t, err := d.st.Get(taskID)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, err)
		return
	}
	if !strings.EqualFold(t.Runner, "claude") {
		writeJSONErr(w, http.StatusConflict, fmt.Errorf("%s runs %q and only a claude card takes /model",
			t.WireName, t.Runner))
		return
	}
	run := d.sup.get(taskID)
	if run == nil || d.sessionGone(t) {
		writeJSONErr(w, http.StatusConflict, fmt.Errorf("atrium does not own a terminal for %s, so there is "+
			"nowhere to type /model. relaunch it under atrium first", t.WireName))
		return
	}
	by := strings.TrimSpace(body.From)
	if by == "" {
		by = "the operator"
	}

	waitTurn := d.waitsForTurn(taskID, WhenImmediate)
	d.modelWaits.Delete(taskID)
	typed, err := d.typeModel(taskID, model, waitTurn)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}

	if err := d.st.SetModel(taskID, model); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	state := "waiting"
	if typed {
		state = "typed"
	}
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
		"by": modelSwitchBy, "asked_by": by, "from": t.Model, "to": model, "state": state,
	}); err != nil {
		log.Printf("[atrium] switched %s to %s but could not record it: %v", taskID, model, err)
	}
	if !typed {
		d.waitForModel(taskID, &modelWait{model: model, by: by})
	}
	d.publishTask(taskID)

	out := map[string]any{"card": taskID, "model": model, "from": t.Model, "typed": typed,
		"when": whenWord(waitTurn), "delivered": "terminal"}
	if !typed {
		out["delivered"] = "waiting"
		out["note"] = "recorded on the card. /model is typed once the input line is clear and the card " +
			"is not on a dialog" + map[bool]string{true: ", after its turn ends", false: ""}[waitTurn] + "."
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(out)
}

// typeModel types `/model <model>` and Enter through the say gate. False and no
// error when the gate was shut and nothing was written.
func (d *Daemon) typeModel(taskID, model string, waitTurn bool) (bool, error) {
	run := d.sup.get(taskID)
	if run == nil || d.holdingFrom(taskID, "") || d.act.dialogOpen(taskID) || d.turnHolds(taskID, waitTurn) {
		return false, nil
	}
	// No paste markers: it is one short line, and a slash command is read as one
	// from keystrokes without depending on how the runner treats a paste.
	return run.injectPeer("", "/model "+model)
}

// waitForModel retries a switch the gate refused, until it types, is replaced by a
// newer request, the runner goes, or modelWaitMax passes.
func (d *Daemon) waitForModel(taskID string, mw *modelWait) {
	d.modelWaits.Store(taskID, mw)
	deadline := time.Now().Add(modelWaitMax)
	go func() {
		for {
			time.Sleep(modelRetryEvery)
			cur, ok := d.modelWaits.Load(taskID)
			if !ok || cur != mw || d.windingDown.Load() || time.Now().After(deadline) {
				return
			}
			run := d.sup.get(taskID)
			if run == nil {
				d.modelWaits.CompareAndDelete(taskID, mw)
				return
			}
			select {
			case <-run.done:
				d.modelWaits.CompareAndDelete(taskID, mw)
				return
			default:
			}
			typed, err := d.typeModel(taskID, mw.model, d.waitsForTurn(taskID, WhenImmediate))
			if err != nil {
				log.Printf("[atrium] typing /model into %s failed: %v", taskID, err)
				continue
			}
			if !typed {
				continue
			}
			if !d.modelWaits.CompareAndDelete(taskID, mw) {
				return
			}
			if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
				"by": modelSwitchBy, "asked_by": mw.by, "to": mw.model, "state": "typed after waiting",
			}); err != nil {
				log.Printf("[atrium] typed /model into %s but could not record it: %v", taskID, err)
			}
			d.publishTask(taskID)
			return
		}
	}()
}
