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
// session is over, so a card goes dead on its own rather than sitting in
// running forever.

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
	// Title and Why are supplied when a session joins by hand, since that is
	// the one moment someone can say what the session is for.
	Title string `json:"title,omitempty"`
	Why   string `json:"why,omitempty"`
}

// handleGate answers the permission hook's question: should this session be
// gated right now?
//
// The hook used to decide from its own environment, which fixed the answer for
// the life of the session. Asking here is what makes join and leave take
// effect immediately. It is one loopback call on a tool the hook was about to
// gate anyway, and an unreachable daemon means no gating, which is the same
// fail-open posture the rest of the hook has.
func (d *Daemon) handleGate(w http.ResponseWriter, r *http.Request) {
	agent := r.URL.Query().Get("agent")
	gated := false
	if agent != "" {
		if on, err := d.st.GatedByWireName(agent); err == nil {
			gated = on
		}
	}
	w.Header().Set("Content-Type", "application/json")
	if gated {
		_, _ = w.Write([]byte(`{"gate":true}`))
		return
	}
	_, _ = w.Write([]byte(`{"gate":false}`))
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

	// A supervised runner dies with the daemon, which owns its pseudo terminal.
	// The id the harness resumes from turns that into a restart rather than a
	// loss, so it is stored on every event rather than only at start.
	if err := d.st.SetResumeID(task.ID, in.Resume); err != nil {
		return err
	}

	switch in.Event {
	case "join":
		// Opting in while running. Gating is state from here on, so the very
		// next tool call is gated without the session restarting.
		if err := d.st.SetGated(task.ID, true); err != nil {
			return err
		}
		if in.Why != "" {
			if err := d.st.SetWhy(task.ID, in.Why); err != nil {
				return err
			}
		}
		if in.Title != "" {
			if err := d.st.SetOverrides(task.ID, map[string]string{"title": in.Title}); err != nil {
				return err
			}
		}
		if err := d.st.AppendEvent(task.ID, store.EventLaunched, map[string]any{
			"by": "join", "pid": in.PID,
		}); err != nil {
			return err
		}
		// Joining is an explicit statement that this session is active now, so
		// it revives the card from any resting state. Done included: leaving
		// marks a card done, and rejoining after that has to bring it back or
		// the gate would stay off for a session that just asked to be gated.
		if task.Status != store.StatusRunning && task.Status != store.StatusNeedsInput &&
			task.Status != store.StatusNeedsPermission {
			if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
				return err
			}
		}
		log.Printf("[atrium] %s joined", in.Agent)

	case "leave":
		// Handing the session back to itself. The card is kept, because the
		// history of what it did is worth having, but nothing is gated and
		// nothing is waiting on a human.
		if err := d.st.SetGated(task.ID, false); err != nil {
			return err
		}
		if _, err := d.CancelPending(task.ID, "this session left atrium"); err != nil {
			return err
		}
		if err := d.st.AppendEvent(task.ID, store.EventExited, map[string]any{
			"by": "leave",
		}); err != nil {
			return err
		}
		if task.Status != store.StatusShelved && task.Status != store.StatusDone {
			if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
				return err
			}
		}
		log.Printf("[atrium] %s left", in.Agent)

	case "end":
		// Left behind, the last activity would have the card claiming to run a
		// tool inside a process that has exited.
		d.act.forget(task.ID)
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
		// Starting revives a card marked dead, which is how a resume arrives.
		if task.Status == store.StatusDead || task.Status == store.StatusBacklog {
			if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
				return err
			}
		}
	}
	d.publishTask(task.ID)
	return nil
}
