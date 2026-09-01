package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// Sessions announce themselves through hooks rather than through the model.
//
// Waiting for the first gated tool call means a session sitting at its prompt
// is invisible, and telling the model to "register with atrium" would spend a
// turn on something the harness already knows. SessionStart and SessionEnd are
// hook events, so both are free and both fire at exactly the right moment.
//
// SessionEnd is the more valuable half: it is the only reliable signal that a
// session is over, which is what makes a card go dead on its own rather than
// sitting in running forever.

// SessionEvent is what a session hook posts.
type SessionEvent struct {
	Agent string `json:"agent"`
	// Event is "start" or "end".
	Event  string `json:"event"`
	Runner string `json:"runner,omitempty"`
	Cwd    string `json:"cwd,omitempty"`
	PID    int    `json:"pid,omitempty"`
	// TaskID binds a launched runner to the card that launched it.
	TaskID string `json:"task_id,omitempty"`
	// Resume is the runner's own session id, so a card can be picked back up.
	Resume string `json:"resume,omitempty"`
	// Source is the harness's own word for why the session started, such as
	// startup, resume or clear. Recorded, not interpreted.
	Source string `json:"source,omitempty"`
}

func (d *Daemon) handleSession(w http.ResponseWriter, r *http.Request) {
	var in SessionEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad json: "+err.Error(), http.StatusBadRequest)
		return
	}
	if strings.TrimSpace(in.Agent) == "" && in.TaskID == "" {
		http.Error(w, "agent or task_id required", http.StatusBadRequest)
		return
	}
	if err := d.onSession(in); err != nil {
		// Registration is best effort. A session must never fail to start
		// because atrium could not record it.
		log.Printf("[atrium] session %s from %s: %v", in.Event, in.Agent, err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))
}

func (d *Daemon) onSession(in SessionEvent) error {
	runner := in.Runner
	if runner == "" {
		runner = "claude"
	}
	obs := observedFor(in.Agent)
	obs.Runner = runner
	obs.PID = in.PID
	if in.Cwd != "" {
		obs.Worktree = strings.ReplaceAll(in.Cwd, `\`, "/")
	}

	// A launched runner already has a card waiting for it.
	var task *store.Task
	if in.TaskID != "" {
		t, err := d.st.Get(in.TaskID)
		if err == nil {
			task = t
		}
	}
	if task == nil {
		t, _, err := d.st.Register(obs)
		if err != nil {
			return err
		}
		task = t
	} else if _, _, err := d.st.Register(obs); err != nil {
		return err
	}

	switch in.Event {
	case "end":
		if err := d.st.AppendEvent(task.ID, store.EventExited, map[string]any{
			"by": "session hook", "source": in.Source,
		}); err != nil {
			return err
		}
		// A card put down by hand stays where it was put.
		if task.Status != store.StatusShelved && task.Status != store.StatusDone {
			if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
				return err
			}
		}
	default:
		if err := d.st.AppendEvent(task.ID, store.EventLaunched, map[string]any{
			"by": "session hook", "source": in.Source, "pid": in.PID,
		}); err != nil {
			return err
		}
		// Starting revives a card that had been marked dead, which is what a
		// resume looks like from here.
		if task.Status == store.StatusDead || task.Status == store.StatusBacklog {
			if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
				return err
			}
		}
	}
	d.publishTask(task.ID)
	return nil
}
