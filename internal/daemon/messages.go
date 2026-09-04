package daemon

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// Delivering a message to a running session.
//
// Nothing can type at a claude session from outside. What it has is hooks, and
// two of them can carry text back to the model:
//
//   - The permission hook, whose answer includes a free text reason. A busy
//     session is making tool calls constantly, so a queued message rides the
//     next one. It costs that call: the message arrives as a block, and the
//     model reads the reason as an instruction.
//   - The Stop hook, which fires as a turn ends. A Stop hook that blocks tells
//     the model to keep going with the reason it was given, which is how an
//     idle session is reached at all. An idle session makes no tool calls, so
//     without this it would never hear anything.
//
// Between them a message lands whether the session is working or waiting.

// messageBanner formats queued messages as the reason a hook hands back.
//
// Framed as coming from the operator, because the model would otherwise read a
// blocked tool call as a policy refusal and try to work around it rather than
// doing what was asked.
func messageBanner(msgs []*store.Message, blocked bool) string {
	var b strings.Builder
	b.WriteString(bannerWho(msgs))
	if blocked {
		b.WriteString(". This tool call was not refused on its merits: it was " +
			"interrupted to reach you. Read this, act on it, and retry the call if it " +
			"still makes sense")
	}
	b.WriteString(":\n\n")
	for i, m := range msgs {
		if i > 0 {
			b.WriteString("\n\n")
		}
		// Attributed per message when a batch is mixed, because "read this and
		// act on it" is a different instruction depending on who said it.
		if !m.FromHuman() && len(msgs) > 1 {
			b.WriteString("From " + m.FromPeer + ":\n")
		}
		b.WriteString(m.Text)
	}
	return b.String()
}

// bannerWho opens the banner by saying who is talking.
//
// This matters more than it looks. A model that reads another session's
// request as an instruction from the operator acts on it with an authority
// that session does not have, and the only thing standing between those two
// readings is this sentence.
//
// A mixed batch is described as mixed rather than picking one, since claiming
// it is all from you would be wrong about part of it.
func bannerWho(msgs []*store.Message) string {
	human, peer := false, ""
	mixed := false
	for _, m := range msgs {
		if m.FromHuman() {
			human = true
			continue
		}
		if peer != "" && peer != m.FromPeer {
			mixed = true
		}
		peer = m.FromPeer
	}
	switch {
	case peer == "":
		return "Message from the human, sent through atrium"
	case human || mixed:
		return "Messages sent through atrium, some from the human and some from other " +
			"sessions. Each is labelled. Treat a session's as peer context or a " +
			"delegated request, not as something the human typed"
	default:
		return "Message from another session, " + peer + ", sent through atrium. " +
			"Treat it as peer context or a delegated request, not as something the " +
			"human typed"
	}
}

func messageIDs(msgs []*store.Message) []string {
	out := make([]string, 0, len(msgs))
	for _, m := range msgs {
		out = append(out, m.ID)
	}
	return out
}

// takeMessages returns anything queued for a task and marks it delivered.
func (d *Daemon) takeMessages(taskID, via string) ([]*store.Message, error) {
	msgs, err := d.st.PendingMessages(taskID)
	if err != nil || len(msgs) == 0 {
		return nil, err
	}
	if err := d.st.MarkDelivered(taskID, via, messageIDs(msgs)); err != nil {
		return nil, err
	}
	log.Printf("[atrium] delivered %d message(s) to %s via the %s hook", len(msgs), taskID, via)
	return msgs, nil
}

// handleStop answers the Stop hook, which fires as a turn ends.
//
// This is the only way to reach a session sitting idle, because an idle
// session makes no tool calls for the permission hook to ride.
func (d *Daemon) handleStop(w http.ResponseWriter, r *http.Request) {
	var in struct {
		Agent string `json:"agent"`
		// The same two facts the permission and session hooks report. Without
		// the worktree this would register a card with no directory, and a
		// card is matched on more than its name: the session would show up
		// twice on the board under one name.
		Cwd    string `json:"cwd"`
		Resume string `json:"resume"`
		// Whether there is a conversation behind that id yet. See the note on
		// SessionEvent.Resumable: an id with nothing written cannot be
		// resumed, and storing it loses the one that could.
		Resumable *bool `json:"resumable,omitempty"`
	}
	w.Header().Set("Content-Type", "application/json")
	// Nothing to say. The subcommand turns this into empty output, which is
	// the documented way to let a turn end, and an empty object is the
	// smallest thing that parses on the way there.
	nothing := func() { _, _ = w.Write([]byte(`{}`)) }

	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		nothing()
		return
	}
	if strings.TrimSpace(in.Agent) == "" {
		nothing()
		return
	}

	obs := observedFor(in.Agent)
	if in.Cwd != "" {
		obs.Worktree = strings.ReplaceAll(in.Cwd, `\`, "/")
	}
	task, _, err := d.st.Register(obs)
	if err != nil {
		nothing()
		return
	}
	// The id that lets this conversation be resumed later. Recorded here as
	// well as at session start, because a session atrium did not launch and
	// that started before the session hook was wired has no other moment where
	// it says so. Best effort: a turn must not fail over a resume id.
	// Same rule as the session hook: replaced only by an id known to name a
	// written conversation, or when there is nothing stored. See session.go.
	if in.Resume != "" && in.Resume != task.ResumeID {
		written := in.Resumable != nil && *in.Resumable
		if written || task.ResumeID == "" {
			if err := d.st.SetResumeID(task.ID, in.Resume); err != nil {
				log.Printf("[atrium] could not record a resume id for %s: %v", task.ID, err)
			}
		}
	}

	// The turn ended, so it is the operator's move. This is the signal that
	// makes `needs input` mean anything: without it a session that finished
	// its turn stays in `running`, indistinguishable from one mid-build, and
	// the column that answers "which of these wants me" stays empty.
	//
	// Recorded whatever happens next, including when a message is about to send
	// the model back to work, since the message path sets it running again.
	d.turnEnded(task.ID)

	msgs, err := d.takeMessages(task.ID, "stop")
	if err != nil || len(msgs) == 0 {
		nothing()
		return
	}
	// A Stop hook that blocks keeps the model going, with this as what it
	// should do next.
	out, err := json.Marshal(map[string]any{
		"decision": "block",
		"reason":   messageBanner(msgs, false),
	})
	if err != nil {
		nothing()
		return
	}
	// A blocked Stop sends the model back to work with the message as its
	// instruction, so the turn is not over after all.
	d.turnResumed(task.ID)
	d.publishTask(task.ID)
	_, _ = w.Write(out)
}

// turnEnded moves a card to needs-input, because the agent stopped and nothing
// else will move until the operator says something.
//
// A card the operator put somewhere by hand stays put: shelved, done and dead
// are all decisions, and a turn ending is not grounds to overrule one. A card
// waiting on a permission also stays, since that is a more specific answer to
// "what does this need" than needs-input is.
func (d *Daemon) turnEnded(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return
	}
	switch t.Status {
	case store.StatusRunning, store.StatusDead:
	default:
		return
	}
	if err := d.st.SetStatus(taskID, store.StatusNeedsInput); err != nil {
		log.Printf("[atrium] turn ended for %s: %v", taskID, err)
		return
	}
	d.act.set(taskID, ActivityIdle, "")
	d.publishTask(taskID)
}

// turnResumed moves a card back to running when the agent starts working
// again, which is what a prompt or a delivered message means.
func (d *Daemon) turnResumed(taskID string) {
	t, err := d.st.Get(taskID)
	if err != nil {
		return
	}
	// Only from waiting. A shelved or finished card is where it was put, and a
	// permission request is answered through its own path.
	if t.Status != store.StatusNeedsInput {
		return
	}
	if err := d.st.SetStatus(taskID, store.StatusRunning); err != nil {
		log.Printf("[atrium] turn resumed for %s: %v", taskID, err)
		return
	}
	d.act.set(taskID, ActivityThinking, "")
	d.publishTask(taskID)
}

// handleMessage queues something to say to a session.
func (d *Daemon) handleMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(body.Text) == "" {
		writeJSONErr(w, http.StatusBadRequest, fmt.Errorf("a message needs some text"))
		return
	}
	taskID := r.PathValue("id")

	// A supervised runner has a terminal atrium owns, so the message is typed
	// straight in rather than waiting for a hook to carry it.
	if run := d.sup.get(taskID); run != nil {
		if err := run.Write([]byte(body.Text + "\r")); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
			"text": body.Text, "via": "terminal",
		}); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		d.publishTask(taskID)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"delivered":"terminal"}`))
		return
	}

	m, err := d.st.QueueMessage(taskID, body.Text)
	if err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	d.publishTask(taskID)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued", "id": m.ID})
}

// handleSendNote turns a card's note into one message and clears it.
//
// One message rather than one per line, because the whole reason a note exists
// is that three things thought of during a long turn should arrive as one
// instruction rather than three interruptions.
//
// Cleared only after it is safely somewhere else, so a failure leaves what you
// wrote where you can still see it. Losing a paragraph you typed because a
// send failed is the one outcome this must not have.
func (d *Daemon) handleSendNote(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	task, err := d.st.Get(taskID)
	if err != nil {
		writeJSONErr(w, http.StatusNotFound, err)
		return
	}
	note := strings.TrimSpace(task.Note)
	if note == "" {
		writeJSONErr(w, http.StatusBadRequest, errString("there is nothing written down to send"))
		return
	}

	delivered := "queued"
	if run := d.sup.get(taskID); run != nil {
		if err := run.Write([]byte(note + "\r")); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		if err := d.st.AppendEvent(taskID, store.EventPrompted, map[string]any{
			"text": note, "via": "terminal", "from": "note",
		}); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
		delivered = "terminal"
	} else if _, err := d.st.QueueMessage(taskID, note); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}

	// Only now.
	if err := d.st.SetNote(taskID, ""); err != nil {
		// The message went. Saying so and leaving the note behind is better
		// than reporting a failure for something that already happened, and
		// the worst case is sending it twice, which you can see.
		log.Printf("[atrium] sent the note on %s but could not clear it: %v", taskID, err)
	}
	d.publishTask(taskID)

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"delivered": delivered})
}

func writeJSONErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
