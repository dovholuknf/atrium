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
	// Resumable is whether there is a conversation behind that id yet.
	//
	// A pointer, so "the hook did not say" is distinguishable from "the hook
	// said no". An older hook binary sends neither and is trusted, which is
	// what it was before this existed.
	Resumable *bool `json:"resumable,omitempty"`
	// Source is the harness's own word for why the session started, such as
	// startup, resume or clear. Recorded, not interpreted.
	Source string `json:"source,omitempty"`
	// Reason is the harness's word for why a session ENDED. `clear` and
	// `resume` both mean another session is starting immediately in the same
	// place, so the card should not die and come back a second later.
	//
	// The hook already declines to post those, and this is checked again here
	// anyway: the daemon cannot assume which version of the hook binary is
	// installed, and an old one posting an end for a clear would flicker a
	// card out of the column somebody was reading.
	Reason string `json:"reason,omitempty"`
	// Trigger is why a compaction happened: `auto` or `manual`. Recorded and
	// not acted on. The interesting part is that it happened at all.
	Trigger string `json:"trigger,omitempty"`
	// Title and Why are supplied when a session joins by hand, since that is
	// the one moment someone can say what the session is for.
	Title string `json:"title,omitempty"`
	Why   string `json:"why,omitempty"`
}

// EndsTheSession reports whether a SessionEnd reason is really the end.
//
// `clear` and `resume` are both followed immediately by a SessionStart in the
// same directory, so treating either as a death makes the card go to finished
// and come straight back, which on the board is a card flickering out of the
// column you were reading.
//
// Everything else, including an unset reason, IS an ending. A runner that says
// nothing has not claimed the session is continuing, and a card left in
// running forever is the worse mistake of the two.
//
// Exported and used by the hook as well as by the daemon, so the two cannot
// disagree about what an ending is.
func EndsTheSession(reason string) bool {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "clear", "resume":
		return false
	}
	return true
}

// orWord is a fallback for a word a runner did not supply.
func orWord(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
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
	// loss.
	//
	// An id is stored only when there is a conversation behind it.
	//
	// A session that opened and was never typed into has no transcript, and
	// resuming its id answers "no conversation found". Storing it anyway
	// replaced a real conversation with one that can never work, so the next
	// start failed, began again empty, and recorded another dud. The thread
	// was lost a little more every restart.
	//
	// "Which event" was tried first and is not enough: a fresh session that
	// does nothing still ends, and its end carried the same useless id. The
	// hook answers the real question, because the transcript is a file it can
	// look at. A hook that says nothing is trusted, which is what every hook
	// did before this existed.
	// One rule: a stored resume id is replaced only by one that is KNOWN to
	// name a written conversation.
	//
	// The failure this closes took several attempts because it has several
	// doors. An id is reported at session start, at session end, on join, and
	// by the Stop hook, and a session that opened and was never typed into
	// reports the same useless id through all of them. Guarding one door moved
	// the leak to the next. Resuming that id answers "no conversation found",
	// atrium starts fresh, the fresh session reports its own equally useless
	// id, and the thread is a little further away every restart.
	//
	// So the test is not which event, and not whether the id looks new. It is
	// whether anybody has confirmed there is something behind it. The hooks
	// answer that by looking for the transcript, which is a file on the
	// machine the session is running on.
	//
	// A card with nothing stored takes whatever it is offered, because an id
	// that might not work still beats no id at all.
	if in.Resume != "" && in.Resume != task.ResumeID {
		written := in.Resumable != nil && *in.Resumable
		if written || task.ResumeID == "" {
			if err := d.st.SetResumeID(task.ID, in.Resume); err != nil {
				return err
			}
		} else {
			log.Printf("[atrium] %s offered a resume id with nothing written behind it, keeping %s",
				in.Agent, task.ResumeID)
		}
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

	case "compact":
		// A moment, not a state. The card records that this session forgot
		// something and stays exactly where it was: nothing about compacting
		// changes whether it wants a human, and moving it would take a card
		// out of the column somebody is reading for a reason that is not about
		// them.
		if err := d.st.AppendEvent(task.ID, store.EventCompacted, map[string]any{
			"trigger": in.Trigger,
		}); err != nil {
			return err
		}
		log.Printf("[atrium] %s compacted its context (%s)", in.Agent, orWord(in.Trigger, "unsaid"))

	case "end":
		// A clear or a resume is not an ending. Another session starts in the
		// same place immediately, so treating it as a death makes the card go
		// to finished and come straight back.
		//
		// Checked here as well as in the hook because the daemon cannot assume
		// which version of the hook binary is installed.
		if !EndsTheSession(in.Reason) {
			log.Printf("[atrium] %s is %sing rather than ending, so its card stays",
				in.Agent, in.Reason)
			break
		}
		// Left behind, the last activity would have the card claiming to run a
		// tool inside a process that has exited.
		d.act.forget(task.ID)
		if err := d.st.AppendEvent(task.ID, store.EventExited, map[string]any{
			"by": "session hook", "source": in.Source, "reason": in.Reason,
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
		// A session that has just started is READY, not running.
		//
		// SessionStart fires before the session has done anything: it is
		// sitting at its prompt with the cursor blinking, which is the
		// definition of the ready column and the opposite of running. Landing
		// in running meant every terminal opened all morning claimed to be
		// working, and the one column that says "these want you" was missing
		// exactly the sessions that most obviously did.
		//
		// Nothing has to undo this. The first prompt or the first tool call
		// both call turnResumed, so the card moves to running the moment the
		// session actually does something. See activity.go.
		//
		// A card put down by hand stays put: shelving is an answer, and a
		// session starting inside a shelved worktree does not overrule it.
		switch task.Status {
		case store.StatusShelved:
		default:
			// Ready because it has not started, which is the opposite of the
			// other way into this column. Without the reason the board told
			// you a session had "finished its turn and wants your next
			// instruction" seconds after you launched it, announcing work it
			// had not begun.
			if err := d.st.SetStatusBecause(task.ID, store.StatusNeedsInput,
				store.WaitingStarted); err != nil {
				return err
			}
		}
	}
	d.publishTask(task.ID)
	return nil
}
