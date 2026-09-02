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
	b.WriteString("Message from the human, sent through atrium")
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
		b.WriteString(m.Text)
	}
	return b.String()
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
	}
	w.Header().Set("Content-Type", "application/json")
	nothing := func() { _, _ = w.Write([]byte(`{"continue":true}`)) }

	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		nothing()
		return
	}
	if strings.TrimSpace(in.Agent) == "" {
		nothing()
		return
	}

	task, _, err := d.st.Register(observedFor(in.Agent))
	if err != nil {
		nothing()
		return
	}
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
	d.publishTask(task.ID)
	_, _ = w.Write(out)
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

func writeJSONErr(w http.ResponseWriter, code int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
