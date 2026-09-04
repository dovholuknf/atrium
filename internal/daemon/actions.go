package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// Running an action on a card.
//
// The delivery path already existed and is the message queue: typed into the
// terminal when atrium owns one, queued for the next hook when it does not.
// What is new is that it comes from a stored prompt and that it can be
// followed by asking the runner to leave.

// exitPause is how long to wait between saying something and asking a runner
// to quit.
//
// Not arbitrary, and not a guess about how long the work takes. The exit keys
// go into the same terminal as the prompt, and a runner reading a line still
// has that line in its input buffer. Sending control-d in the same breath can
// land before the prompt has been submitted, which throws away the
// instruction and quits, doing exactly the opposite of what was asked.
//
// This is the weak part of `and exit` and it is weak by nature: there is no
// signal that says a runner has accepted a line. See the note in the backlog.
const exitPause = 1500 * time.Millisecond

// ActionResult is what happened when an action ran.
type ActionResult struct {
	// Delivered is "terminal" or "queued", the same two words the message
	// endpoint uses, because they are different promises and the board has to
	// say which one it made.
	Delivered string `json:"delivered"`
	// Exiting is whether the runner was also asked to leave.
	Exiting bool `json:"exiting"`
	// Note carries anything the operator should know, such as an exit that
	// could not be attempted.
	Note string `json:"note,omitempty"`
}

// runAction says an action's prompt to a card, and optionally asks the runner
// to leave afterwards.
func (d *Daemon) runAction(taskID, actionID string) (*ActionResult, error) {
	action, err := d.st.CardActionByID(actionID)
	if err != nil {
		return nil, fmt.Errorf("no action %s", actionID)
	}
	task, err := d.st.Get(taskID)
	if err != nil {
		return nil, err
	}
	if !action.Offers(task) {
		// Refused rather than run. A board that offered the right actions and
		// ran any of them would be a filter that only worked when it was
		// looked at.
		return nil, fmt.Errorf("%q is not offered on this card", action.Label)
	}

	out := &ActionResult{}

	// Typed straight in when atrium owns the terminal, queued otherwise. The
	// same two routes the message endpoint takes, and the same reason they are
	// named: typed means it has already landed, queued means it has not and
	// will not until the session makes its next tool call or ends its turn.
	run := d.sup.get(taskID)
	if run != nil {
		if err := run.Write([]byte(action.Prompt + "\r")); err != nil {
			return nil, err
		}
		out.Delivered = "terminal"
	} else {
		if _, err := d.st.QueueMessage(taskID, action.Prompt); err != nil {
			return nil, err
		}
		out.Delivered = "queued"
	}

	if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
		"text": action.Prompt, "via": out.Delivered,
		"action": action.ID, "label": action.Label,
	}); err != nil {
		return nil, err
	}

	if action.After == store.AfterExit {
		switch {
		case run == nil:
			// Nothing to send keys to. Said out loud rather than silently
			// skipped, because "and exit" is half the reason the action was
			// pressed and a session that stays up is the thing to notice.
			out.Note = "atrium does not own this session's terminal, so it was asked to " +
				"finish but cannot be made to quit. it will need closing where it runs."
		default:
			out.Exiting = true
			// After a pause, in the background, so the response does not wait
			// on it. See exitPause.
			go func() {
				time.Sleep(exitPause)
				if err := d.StopRunner(taskID); err != nil {
					log.Printf("[atrium] asking %s to exit after an action: %v", taskID, err)
				}
			}()
		}
	}

	d.publishTask(taskID)
	log.Printf("[atrium] ran %q on %s, %s", action.Label, task.DisplayTitle(), out.Delivered)
	return out, nil
}

// handleRunAction is the board pressing one of the buttons.
func (d *Daemon) handleRunAction(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Action string `json:"action"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if body.Action == "" {
		writeJSONErr(w, http.StatusBadRequest, errString("say which action to run"))
		return
	}
	res, err := d.runAction(r.PathValue("id"), body.Action)
	if err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(res)
}
