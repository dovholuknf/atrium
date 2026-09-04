package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// An agent saying it finished.
//
// The largest hole in what atrium does, and it is a hole in the shape of a
// missing verb. Everything an agent reported landed in `needs-input`, so the
// board could not tell "finished, go and look at the result" from "stuck,
// answer me", and only a human moving a card by hand ever produced `done`.
//
// A command rather than a tool, because a command is the one channel every
// runner already has. An agent that can run `ls` can run `atrium finish`, with
// no MCP server, no tool description and no cooperation from the harness. That
// matters more here than anywhere else in atrium: this has to work for codex
// and for a bare shell, not only for the runner that happens to have a tool
// surface.
//
// Same posture as every other agent-facing endpoint. It answers, it never
// fails a session, and an unknown agent is not an error.

// FinishRequest is a session declaring its work over.
type FinishRequest struct {
	// Agent is the wire name, the same one every hook uses.
	Agent string `json:"agent"`
	// TaskID is used when the session was launched by atrium and told which
	// card it belongs to, which is more reliable than a name.
	TaskID string `json:"task_id,omitempty"`
	// Recap is what it did, in its own words. Optional, and the card says so
	// when it is missing rather than inventing one.
	Recap string `json:"recap,omitempty"`
	// Status is where to put the card. `done` unless something says
	// otherwise, and the only other value that means anything here is
	// `needs-input`, which is a session saying "I am handing this back without
	// claiming it is finished".
	Status string `json:"status,omitempty"`
}

// handleFinish answers a session declaring its work over.
func (d *Daemon) handleFinish(w http.ResponseWriter, r *http.Request) {
	var in FinishRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		writeJSONErr(w, http.StatusBadRequest, err)
		return
	}
	if strings.TrimSpace(in.Agent) == "" && in.TaskID == "" {
		writeJSONErr(w, http.StatusBadRequest,
			errString("say which session this is, with agent or task_id"))
		return
	}

	task, err := d.taskFor(in.Agent, in.TaskID)
	if err != nil {
		// An agent atrium has never heard of is not an error. A session that
		// was never gated is entitled to say it finished and atrium is
		// entitled to have nowhere to put that, and failing here would mean a
		// session could fail at the moment it tried to end tidily.
		log.Printf("[atrium] %s said it finished and has no card here", in.Agent)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"recorded":false}`))
		return
	}

	status := store.StatusDone
	if strings.TrimSpace(in.Status) == store.StatusNeedsInput {
		// Handing the work back without claiming it is over. A different thing
		// and worth being able to say.
		status = store.StatusNeedsInput
	}

	if err := d.st.SetRecap(task.ID, in.Recap); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}
	// Recorded as coming from the session, so `done` that an agent declared
	// and `done` that a human dragged are distinguishable afterwards. They are
	// the same column and they are not the same claim.
	if err := d.st.AppendEvent(task.ID, store.EventSubmitted, map[string]any{
		"by": "agent", "kind": "finished", "status": status,
		"recap": in.Recap != "",
	}); err != nil {
		writeJSONErr(w, http.StatusInternalServerError, err)
		return
	}

	// A card put down by hand stays put. Shelving is an answer, and a session
	// inside a shelved worktree announcing it is done does not overrule it.
	if task.Status != store.StatusShelved {
		if err := d.st.SetStatus(task.ID, status); err != nil {
			writeJSONErr(w, http.StatusInternalServerError, err)
			return
		}
	}
	// Whatever it was doing, it is not doing now.
	d.act.forget(task.ID)
	d.publishTask(task.ID)

	log.Printf("[atrium] %s says it is finished%s", task.DisplayTitle(),
		map[bool]string{true: ", and left a recap", false: " and said nothing about what it did"}[
			strings.TrimSpace(in.Recap) != ""])

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{
		"ok": true, "recorded": true, "task_id": task.ID, "status": status,
	})
}

// taskFor resolves a session to its card, by id when it has one and by wire
// name otherwise.
//
// Never creates one. Everywhere else a session announcing itself is a reason
// to make a card; here it is not, because a card created at the moment a
// session says it finished would be a card holding one fact and no history.
func (d *Daemon) taskFor(agent, taskID string) (*store.Task, error) {
	if taskID != "" {
		if t, err := d.st.Get(taskID); err == nil {
			return t, nil
		}
	}
	return d.st.GetByWireName(d.st.Qualify(agent))
}

// errString is an error from a literal, without reaching for fmt.
type errString string

func (e errString) Error() string { return string(e) }
