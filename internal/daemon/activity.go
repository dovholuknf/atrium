package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
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
	// ActivityCompacting starts with PreCompact. There is no matching completion
	// hook, so clear it on the next activity event or after compactingFor.
	// This is transient activity; store.EventCompacted records the lasting event.
	ActivityCompacting = "compacting"
)

// staleAfter is how long an activity is believed.
//
// Hooks stop arriving without warning when a session is killed mid-tool.
// Without a cutoff that card reads "running Bash" until the daemon restarts.
// Long enough not to write off a slow tool, short enough that a dead session
// stops claiming to be busy.
const staleAfter = 15 * time.Minute

// compactingFor is how long a compaction is believed when nothing has been
// heard since it began.
//
// Long enough to cover a big context being rewritten, short enough that a
// session killed mid-compaction stops claiming it. Past this the card reads
// `thinking`, because compaction happens mid-turn: a session that was
// compacting and has gone quiet is one still working, not one that finished.
const compactingFor = 2 * time.Minute

// Tools whose whole purpose is to put a question to the operator.
//
// Matched by name and case-insensitively, because a harness names its own
// tools and atrium is not going to be told when one is renamed. A name that
// stops matching costs the badge and nothing else, which is the right way for
// this to fail.
//
// Deliberately short. Every entry here is a claim that the tool BLOCKS on a
// person, and a tool that merely produces output a person might read is not
// that. Guessing from a name like `confirm` would mark cards that are not
// waiting on anybody.
var askingTools = map[string]bool{
	"askuserquestion": true,
	"exitplanmode":    true,
}

// IsAskingTool reports whether a tool call is the model asking you something.
func IsAskingTool(tool string) bool {
	return askingTools[strings.ToLower(strings.TrimSpace(tool))]
}

// Activity is one runner's current state. Zero value means nothing is known.
// Subagent is one agent running under a session.
//
// Held in memory with the rest of the activity and never written down, for the
// same reason: it is a fact about a process that is running right now, and it
// would be a lie the moment the daemon restarted.
type Subagent struct {
	// ID pairs a stop with its start. Never shown.
	ID string `json:"id"`
	// Type is what the runner calls it: `explore`, `general-purpose`, the name
	// of a custom agent. This is the part worth reading.
	Type string `json:"type,omitempty"`
	// Since is when it started, so a subagent that has been going for four
	// minutes is distinguishable from one that just began.
	Since time.Time `json:"since"`
	// Seconds is Since as an age, filled in when the activity is served.
	Seconds int64 `json:"seconds"`
}

type Activity struct {
	// What is one of the Activity constants.
	What string `json:"what"`
	// Tool is the tool being run, when What is ActivityTool.
	Tool string `json:"tool,omitempty"`
	// Subagents currently running under this session.
	//
	// A count, kept because it is what the badge shows and because a hook that
	// went missing can leave the named list short without making the number
	// wrong in a way anybody would notice.
	Subagents int `json:"subagents"`
	// Running names them, when the runner says who they are. Claude Code's
	// SubagentStart carries an agent id and a type, so "3 subagents" can be
	// "explore, review, review" instead. Oldest first, which is the order they
	// were started in and the order they will mostly finish in.
	//
	// May be shorter than Subagents. A runner that reports no id still moves
	// the count, and the count is the thing that must not lie.
	Running []Subagent `json:"running,omitempty"`
	// Dialog says the runner has put a prompt on its own screen that atrium
	// did not raise, so nothing may type into that terminal.
	//
	// `runner.Say` writes the text and then writes Enter. An Enter landing on
	// a dialog answers it with whatever option was highlighted, and nothing
	// reports that it happened: the operator sees a message they sent, and
	// separately a tool call approved by nobody.
	//
	// IN MEMORY AND NEVER WRITTEN DOWN, like everything else here. It is true
	// of a process rather than of a card, and it would be a lie the moment the
	// daemon restarted. See docs/activity-design.md.
	Dialog bool `json:"dialog,omitempty"`
	// Since is when this state began, so a card can say how long a tool has
	// been going.
	Since time.Time `json:"since"`
	// Seconds is Since as an age, filled in when the activity is served.
	Seconds int64 `json:"seconds"`
}

// activityTracker holds one Activity per task, and one Telemetry beside it.
//
// Both live here rather than in two structures because they die together: a
// session ending drops everything atrium believed about a running process, and
// one `forget` that covers both cannot be half-remembered at a new call site.
// They expire on different clocks, which is `telemetry` and not this.
type activityTracker struct {
	mu  sync.Mutex
	now func() time.Time
	by  map[string]*Activity
	// tel is the statusline figure per task. See telemetry.go.
	tel map[string]*Telemetry
	// telAt is when each CALLER last got a post accepted, for the floor. Keyed
	// by what the caller said it was, not by a card id.
	telAt map[string]time.Time
}

func newActivityTracker() *activityTracker {
	return &activityTracker{
		now:   time.Now,
		by:    map[string]*Activity{},
		tel:   map[string]*Telemetry{},
		telAt: map[string]time.Time{},
	}
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
	// The inferred end of a compaction. See ActivityCompacting: there is no
	// hook for it, so the badge stops on the clock rather than on an event.
	// The age is not reset, because the session has been working since the
	// compaction began and that is what the card is reporting.
	if out.What == ActivityCompacting && age > compactingFor {
		out.What = ActivityThinking
	}
	// Copied, not shared. The caller serialises this outside the lock, and
	// handing over the live slice would race a subagent starting.
	if len(cur.Running) > 0 {
		out.Running = make([]Subagent, len(cur.Running))
		for i, s := range cur.Running {
			s.Seconds = int64(a.now().Sub(s.Since).Seconds())
			out.Running[i] = s
		}
	}
	// THE COUNT CAN BE LOWER THAN THE LIST, AND THAT IS DELIBERATE.
	//
	// An unknown stop takes the tally down and finds nothing to remove, which
	// `TestAnUnknownStopStillCountsDown` pins on purpose: the stop is a fact
	// even when the start that would have named it was lost, and the agent it
	// could not match is still the honest thing to keep listing.
	//
	// So `"subagents": 0` beside a `running` array with an entry in it is a
	// state this is allowed to serve, and it was observed in the wild. Any
	// client deciding whether there is anything to SHOW has to look at both.
	// The board's badge gated on the count alone and drew nothing for a
	// session that had subagents running, which is the report this note comes
	// from. Clamping here was tried and is wrong: it would make the number
	// claim an agent had not finished when the runner said it had.
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
	// ANYTHING HAPPENING MEANS THE DIALOG HAS GONE.
	//
	// There is no hook for a prompt being dismissed, so the flag is cleared by
	// the next thing the session does: a tool call, a turn ending, a prompt
	// submitted. Same shape as the compaction badge, which has the same
	// problem and clears the same way.
	//
	// `dialogRaised` sets the flag AFTER calling this, so the ordering there
	// is load bearing and says so.
	cur.Dialog = false
}

// hasPendingPermission reports whether atrium is itself holding a request for
// this card.
//
// The one fact that tells atrium's own prompt apart from the runner's. A store
// failure answers TRUE, which suppresses the flag: the cost of a false positive
// is a message queued that could have been typed, and the cost of a false
// negative is typing into a dialog. Those are not the same size.
func (d *Daemon) hasPendingPermission(taskID string) bool {
	pending, err := d.st.PendingForTask(taskID)
	if err != nil {
		return true
	}
	return len(pending) > 0
}

// dialogRaised records that the runner has a prompt on its own screen.
//
// Called only for a notification atrium did not raise. The caller establishes
// that, because it is the only thing that knows whether a request of its own is
// pending on this card.
func (a *activityTracker) dialogRaised(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		cur = &Activity{Since: a.now()}
		a.by[taskID] = cur
	}
	cur.Dialog = true
}

// dialogOpen reports whether anything may type into this card's terminal.
//
// Stale is impossible in the direction that matters. The flag is cleared by the
// next activity of any kind, and a session that is drawing a dialog is by
// definition doing nothing else, so a true answer here is either current or
// belongs to a session that has said nothing since. The expensive mistake is
// the other direction, and this does not make it.
func (a *activityTracker) dialogOpen(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	return cur != nil && cur.Dialog
}

// addSubagents moves the tally, never below zero.
//
// SubagentStart takes it up and SubagentStop takes it down. A hook is best
// effort, so a dropped one puts the count out of step, and a negative count
// renders as nonsense, so the floor is enforced here.
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

// subagentStarted records who, as well as how many.
//
// The count moves whether or not a name came with it, because the count is the
// thing that must not lie. A runner that reports no id contributes to the
// number and not to the list, which reads as "3 subagents: explore, review"
// and is honest about knowing two of the three.
func (a *activityTracker) subagentStarted(taskID, id, kind string) {
	a.addSubagents(taskID, 1)
	if id == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		return
	}
	// A repeated start is the same subagent, not a second one. Hooks are best
	// effort and retried, and two rows for one agent is the kind of wrong that
	// looks like real work happening.
	for _, s := range cur.Running {
		if s.ID == id {
			return
		}
	}
	cur.Running = append(cur.Running, Subagent{ID: id, Type: kind, Since: a.now()})
}

// subagentStopped drops it from the list and the tally.
//
// An unknown id still moves the count. The stop is a fact even when the start
// that would have named it was lost, and refusing to count it down would leave
// a session claiming a subagent that finished.
func (a *activityTracker) subagentStopped(taskID, id string) {
	a.addSubagents(taskID, -1)
	if id == "" {
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		return
	}
	for i, s := range cur.Running {
		if s.ID == id {
			cur.Running = append(cur.Running[:i], cur.Running[i+1:]...)
			return
		}
	}
}

// forget drops everything known about a task's running process, for when a
// session ends.
//
// The context figure goes with the activity. It described a conversation that
// has ended, and a card that says "92% context" about a session that is no
// longer there is worse than a card that says nothing: it is the number that
// decides which agent you go and look at.
func (a *activityTracker) forget(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.by, taskID)
	delete(a.tel, taskID)
}

// ActivityEvent is what a hook posts to /activity.
type ActivityEvent struct {
	Agent  string `json:"agent"`
	TaskID string `json:"task_id,omitempty"`
	// Event is tool-start, tool-end, prompt, subagent-start, subagent-end,
	// idle or waiting.
	//
	// idle and waiting both mean the agent stopped and it is the operator's
	// move: idle from a turn ending, waiting from the Notification hook, which
	// fires when claude has been sitting on a prompt. Both move the card to
	// needs-input, which is what makes that column mean anything.
	Event string `json:"event"`
	// Tool is the tool name on a tool-start.
	Tool string `json:"tool,omitempty"`
	// AgentID and AgentType name a subagent, on subagent-start and
	// subagent-end. Empty on everything else, and empty from a runner that
	// does not report them, which costs the name and not the count.
	AgentID   string `json:"agent_id,omitempty"`
	AgentType string `json:"agent_type,omitempty"`
	// Notification is which kind of Notification hook fired, on a `waiting`.
	// Empty on everything else and empty from a runner that does not send it.
	//
	// The one value acted on is `permission_prompt`, which says a dialog is on
	// that terminal's screen. See dialogRaised.
	Notification string `json:"notification,omitempty"`
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
		// A tool call is work, so a card that was waiting is not any more.
		d.turnResumed(taskID)
		// ASKING is a tool, which is the whole reason this is knowable.
		//
		// Until now a card in `ready` could not say whether the model had put
		// a question to you or had simply run out of things to do, and those
		// want very different amounts of hurry: one is a person blocking an
		// agent, the other is an agent that finished. There was no signal for
		// it, because "the turn ended" is all the Stop hook says.
		//
		// But Claude Code asks by CALLING A TOOL, and PreToolUse fires for it
		// with the name, which atrium already receives. So the question mark
		// is recorded here, at the moment it is asked, and the card carries it
		// into the wait that follows.
		//
		// Best effort like everything else on this path. A failure to record
		// it must not fail a tool call. See docs/activity-design.md.
		if IsAskingTool(in.Tool) {
			if err := d.st.NoteAsked(taskID); err != nil {
				log.Printf("[atrium] could not record a question from %s: %v", in.Agent, err)
			}
		}
	case "tool-end":
		// The turn continues, so the model has the floor again.
		d.act.set(taskID, ActivityThinking, "")
	case "prompt":
		// The operator answered, so the agent is working again. This is the
		// other half of the needs-input signal: without it a card stays in
		// needs-input for the rest of the session.
		d.act.set(taskID, ActivityThinking, "")
		d.turnResumed(taskID)
	case "subagent-start":
		d.act.subagentStarted(taskID, in.AgentID, in.AgentType)
	case "subagent-end":
		d.act.subagentStopped(taskID, in.AgentID)
	case "idle", "waiting":
		// NOT the same meaning, which is what this used to say.
		//
		// `idle` is the Stop hook: a turn ended, the agent has nothing more to
		// do, and it will sit there indefinitely costing nothing. `waiting` is
		// the Notification hook, which Claude Code fires when it is BLOCKED ON
		// YOU: a question put to you, or a prompt it cannot get past.
		//
		// Flattening them meant the board could not tell a session that asked
		// you something from one that had simply finished, and both landed in
		// `ready` reading the same. That was the gap.
		d.act.set(taskID, ActivityIdle, "")
		if in.Event == "waiting" {
			d.turnEndedBecause(taskID, store.WaitingAsked)
			// A PROMPT ON THE RUNNER'S OWN SCREEN, which is not the same as
			// one atrium raised.
			//
			// While atrium's gate holds a request the runner is blocked inside
			// a hook and draws nothing, so a `permission_prompt` arriving with
			// a request of ours pending is our own echo and says nothing new.
			// Arriving WITHOUT one, it means the runner put a dialog up by
			// itself: a session with the gate off, a trust prompt, a plan
			// approval. That terminal must not be typed into, because `Say`
			// ends with an Enter and an Enter answers a dialog.
			//
			// After `set`, which clears the flag. Reversing these two lines
			// raises the dialog and then immediately forgets it.
			if in.Notification == "permission_prompt" && !d.hasPendingPermission(taskID) {
				d.act.dialogRaised(taskID)
			}
		} else {
			d.turnEnded(taskID)
		}
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
