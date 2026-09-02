package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"
)

// What a runner is doing right now, as opposed to what it needs from a human.
//
// Cards in `running` are indistinguishable whether the session is grinding
// through a build, thinking, waiting on subagents, or hung. Without this the
// only way to tell is to attach.
//
// Not stored. After a daemon restart a stored "running Bash" would describe a
// process that no longer exists.
//
// See docs/activity-design.md.

// Activity states. Status says what a card needs; these say what it is doing.
// Waiting is absent: the column and the wait chip already carry it.
const (
	ActivityThinking = "thinking"
	ActivityTool     = "tool"
	ActivityIdle     = "idle"
)

// staleAfter is how long an activity is believed.
//
// Hooks stop arriving without warning when a session is killed mid-tool.
// Without a cutoff that card reads "running Bash" until the daemon restarts.
// Long enough not to write off a slow tool, short enough that a dead session
// stops claiming to be busy.
const staleAfter = 15 * time.Minute

// Activity is one runner's current state. Zero value means nothing is known.
type Activity struct {
	// What is one of the Activity constants.
	What string `json:"what"`
	// Tool is the tool being run, when What is ActivityTool.
	Tool string `json:"tool,omitempty"`
	// Subagents currently running under this session.
	Subagents int `json:"subagents"`
	// Since is when this state began, so a card can say how long a tool has
	// been going.
	Since time.Time `json:"since"`
	// Seconds is Since as an age, filled in when the activity is served.
	Seconds int64 `json:"seconds"`
}

// activityTracker holds one Activity per task.
type activityTracker struct {
	mu  sync.Mutex
	now func() time.Time
	by  map[string]*Activity
}

func newActivityTracker() *activityTracker {
	return &activityTracker{now: time.Now, by: map[string]*Activity{}}
}

// get returns a copy of a task's activity, or nil when no events have arrived
// or the last one is past the cutoff.
func (a *activityTracker) get(taskID string) *Activity {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		return nil
	}
	age := a.now().Sub(cur.Since)
	if age > staleAfter {
		return nil
	}
	out := *cur
	out.Seconds = int64(age.Seconds())
	return &out
}

// set replaces the activity, keeping the subagent count: that is a running
// tally, not part of the state being replaced.
func (a *activityTracker) set(taskID, what, tool string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		cur = &Activity{}
		a.by[taskID] = cur
	}
	// An unchanged state keeps its start time, so "thinking for 3 minutes"
	// counts from the thinking, not from the last hook.
	if cur.What != what || cur.Tool != tool {
		cur.Since = a.now()
	}
	cur.What, cur.Tool = what, tool
}

// addSubagents moves the tally, never below zero.
//
// Claude Code has no hook for a subagent starting, so the count is inferred: a
// Task tool call is one starting, SubagentStop is one ending. Inference drifts,
// and a negative count renders as nonsense, so the floor is enforced here.
func (a *activityTracker) addSubagents(taskID string, delta int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		cur = &Activity{Since: a.now()}
		a.by[taskID] = cur
	}
	cur.Subagents += delta
	if cur.Subagents < 0 {
		cur.Subagents = 0
	}
}

// forget drops a task's activity, for when a session ends.
func (a *activityTracker) forget(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.by, taskID)
}

// ActivityEvent is what a hook posts to /activity.
type ActivityEvent struct {
	Agent  string `json:"agent"`
	TaskID string `json:"task_id,omitempty"`
	// Event is tool-start, tool-end, prompt, subagent-end, idle or waiting.
	//
	// idle and waiting both mean the agent stopped and it is the operator's
	// move: idle from a turn ending, waiting from the Notification hook, which
	// fires when claude has been sitting on a prompt. Both move the card to
	// needs-input, which is what makes that column mean anything.
	Event string `json:"event"`
	// Tool is the tool name on a tool-start.
	Tool string `json:"tool,omitempty"`
}

// handleActivity records what a session is doing.
//
// Rides PreToolUse, the hot path for every tool call every session makes, so it
// answers before doing any work and never blocks. An unknown agent is accepted
// and dropped rather than refused.
func (d *Daemon) handleActivity(w http.ResponseWriter, r *http.Request) {
	// Acknowledged before the body is even read. Nothing about an activity
	// post can produce a non-2xx, including a malformed one: a caller that
	// treats non-2xx as a failure would surface or retry it, and this runs
	// before every tool call in every session.
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))

	var in ActivityEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		log.Printf("[atrium] unreadable activity post: %v", err)
		return
	}

	// Everything after the reply is bookkeeping the caller does not wait on.
	go d.onActivity(in)
}

// onActivity applies one event. Returns the task it landed on, or "" when
// there was nothing to land on.
func (d *Daemon) onActivity(in ActivityEvent) string {
	taskID := in.TaskID
	if taskID == "" {
		name := strings.TrimSpace(in.Agent)
		if name == "" {
			return ""
		}
		t, err := d.st.GetByWireName(name)
		if err != nil {
			// A session with no card has no activity to record. Not logged:
			// this is a hot path.
			return ""
		}
		taskID = t.ID
	}

	switch in.Event {
	case "tool-start":
		d.act.set(taskID, ActivityTool, in.Tool)
		// A Task call is a subagent starting. No hook reports that directly.
		if strings.EqualFold(in.Tool, "Task") {
			d.act.addSubagents(taskID, 1)
		}
		// A tool call is work, so a card that was waiting is not any more.
		d.turnResumed(taskID)
	case "tool-end":
		// The turn continues, so the model has the floor again.
		d.act.set(taskID, ActivityThinking, "")
	case "prompt":
		// The operator answered, so the agent is working again. This is the
		// other half of the needs-input signal: without it a card stays in
		// needs-input for the rest of the session.
		d.act.set(taskID, ActivityThinking, "")
		d.turnResumed(taskID)
	case "subagent-end":
		d.act.addSubagents(taskID, -1)
	case "idle", "waiting":
		// The agent stopped and it is the operator's move. `waiting` is what
		// the Notification hook reports, which fires when claude has been
		// sitting on a prompt; `idle` is a turn ending. Same meaning to a
		// board: this one wants you.
		d.act.set(taskID, ActivityIdle, "")
		d.turnEnded(taskID)
	default:
		log.Printf("[atrium] unknown activity event %q from %s", in.Event, in.Agent)
		return ""
	}

	// The card's row has not changed, so there is nothing to publish from the
	// store. Push the activity itself.
	if a := d.act.get(taskID); a != nil {
		d.ap.Broadcast("activity", map[string]any{"task_id": taskID, "activity": a})
	}
	return taskID
}

// activityFor adapts the tracker to what the api package expects. An absent
// activity returns an untyped nil: a typed nil through an interface serialises
// as a present-but-empty object.
func (d *Daemon) activityFor(taskID string) any {
	a := d.act.get(taskID)
	if a == nil {
		return nil
	}
	return a
}
