package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
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
	if !d.runsClaude(t) {
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
	// ONE SWITCH AT A TIME PER CARD. The wait it replaces, the typing and the new
	// wait are one step: a waiter that checked it was current a moment ago must
	// not type its older model after this one went in.
	mu := d.modelLock(taskID)
	mu.Lock()
	d.modelWaits.Delete(taskID)
	typed, err := d.typeModel(taskID, model, waitTurn)
	if err == nil {
		err = d.st.SetModel(taskID, model)
	}
	var mw *modelWait
	if err == nil && !typed {
		mw = &modelWait{model: model, by: by}
		d.modelWaits.Store(taskID, mw)
	}
	mu.Unlock()
	if err != nil {
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
	if mw != nil {
		d.waitForModel(taskID, mw)
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

// noteTypedModel records a `/model <id>` the operator sent from the terminal.
//
// A SWITCH MADE BY HAND IS THE SAME SWITCH. Left unrecorded, the next resume,
// including the room restart's, launches on whatever the card was started on and
// the model goes back without anybody asking. Only a line that is exactly
// `/model` and one alias (sonnet, opus, haiku, fable) is taken. A bare `/model` opens Claude's picker and
// names nothing, so it records nothing.
func (d *Daemon) noteTypedModel(taskID, line string) {
	f := strings.Fields(line)
	if len(f) != 2 || !strings.EqualFold(f[0], "/model") {
		return
	}
	// ALIASES ONLY. A typed id is unchecked, and a mistyped one stored on the card
	// fails every later resume. A caller of the endpoint chose its id on purpose.
	model, ok := validModel(f[1])
	if !ok || !modelAliases[model] {
		return
	}
	t, err := d.st.Get(taskID)
	if err != nil || !d.runsClaude(t) || t.Model == model {
		return
	}
	mu := d.modelLock(taskID)
	mu.Lock()
	err = d.st.SetModel(taskID, model)
	// A typed switch supersedes one still waiting for the line.
	d.modelWaits.Delete(taskID)
	mu.Unlock()
	if err != nil {
		log.Printf("[atrium] %s was switched to %s by hand but the card could not record it: %v", taskID, model, err)
		return
	}
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
		"by": modelSwitchBy, "asked_by": "the operator", "from": t.Model, "to": model, "state": "typed by hand",
	}); err != nil {
		log.Printf("[atrium] switched %s to %s by hand but could not record it: %v", taskID, model, err)
	}
	d.publishTask(taskID)
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

// modelLock is the card's switch lock. The handler holds it across replacing a wait,
// typing and storing the new one, and a waiter holds it across checking it is still
// the current wait and typing, so the two cannot interleave.
func (d *Daemon) modelLock(taskID string) *sync.Mutex {
	mu, _ := d.modelLocks.LoadOrStore(taskID, &sync.Mutex{})
	return mu.(*sync.Mutex)
}

// runsClaude says whether a card's runner is Claude Code, which is the one runner
// that takes `/model`. DECIDED BY THE HARNESS ROW, not its id, so a `claude-worker`
// or `claude-fable` row that runs the claude command counts. A runner with no row
// falls back to the id.
func (d *Daemon) runsClaude(t *store.Task) bool {
	if t.Runner == "" {
		return false
	}
	if h, err := d.st.Harness(t.Runner); err == nil && h != nil {
		return isClaude(h)
	}
	return strings.EqualFold(t.Runner, "claude")
}

// waitForModel retries a switch the gate refused, until it types, is replaced by a
// newer request, the runner goes, or modelWaitMax passes. The caller has already
// stored mw in modelWaits under the card's lock.
//
// GIVING UP IS RECORDED, once on the card and once in the log: the model is on the
// card and a later start uses it, but the live session never switched.
func (d *Daemon) waitForModel(taskID string, mw *modelWait) {
	deadline := time.Now().Add(modelWaitMax)
	go func() {
		failed := false
		for {
			time.Sleep(modelRetryEvery)
			if d.windingDown.Load() {
				return
			}
			if time.Now().After(deadline) {
				d.giveUpOnModel(taskID, mw)
				return
			}
			mu := d.modelLock(taskID)
			mu.Lock()
			if cur, ok := d.modelWaits.Load(taskID); !ok || cur != mw {
				mu.Unlock()
				return
			}
			run := d.sup.get(taskID)
			gone := run == nil
			if !gone {
				select {
				case <-run.done:
					gone = true
				default:
				}
			}
			if gone {
				d.modelWaits.CompareAndDelete(taskID, mw)
				mu.Unlock()
				return
			}
			typed, err := d.typeModel(taskID, mw.model, d.waitsForTurn(taskID, WhenImmediate))
			if typed {
				d.modelWaits.CompareAndDelete(taskID, mw)
			}
			mu.Unlock()
			if err != nil {
				// Once per wait, not every retry.
				if !failed {
					log.Printf("[atrium] typing /model into %s failed, will keep trying: %v", taskID, err)
					failed = true
				}
				continue
			}
			if !typed {
				continue
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

// giveUpOnModel ends a wait that ran out of time, if it is still the current one.
func (d *Daemon) giveUpOnModel(taskID string, mw *modelWait) {
	mu := d.modelLock(taskID)
	mu.Lock()
	still := d.modelWaits.CompareAndDelete(taskID, mw)
	mu.Unlock()
	if !still {
		return
	}
	log.Printf("[atrium] gave up typing /model %s into %s after %s: the card holds it for the next start",
		mw.model, taskID, modelWaitMax)
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
		"by": modelSwitchBy, "asked_by": mw.by, "to": mw.model, "state": "gave up waiting",
		"waited": modelWaitMax.String(),
	}); err != nil {
		log.Printf("[atrium] could not record the give-up on %s: %v", taskID, err)
	}
	d.publishTask(taskID)
}
