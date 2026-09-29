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
	// HeldPeer names the session whose message is waiting to be typed into this
	// terminal, empty when nothing is held. HeldSeconds is how long it has
	// waited.
	//
	// NOT SUBJECT TO THE STALENESS CUTOFF, unlike everything else here. The rest
	// is about a running process and expires when the hooks go quiet. A held
	// message is a fact about a queued injection, and it stays true while the
	// operator's line is dirty however long that runs, which is exactly when the
	// process is idle and the rest of this has expired. See the held map and
	// `pendingInjector`.
	HeldPeer    string `json:"held_peer,omitempty"`
	HeldSeconds int64  `json:"held_seconds,omitempty"`
	// HeldFor is what is holding the oldest message: one of the HeldFor
	// constants. HeldCount is how many are held, which the chip shows.
	HeldFor   string `json:"held_for,omitempty"`
	HeldCount int    `json:"held_count,omitempty"`
	// HeldTurn says whose rule a turn wait is, when HeldFor is the turn: one of
	// the HeldTurn constants. The board names that one reason.
	HeldTurn string `json:"held_turn,omitempty"`
	// HeldQuiet is a hold that is only the message waiting as it was meant to:
	// every held message waits for the turn, and none has waited past
	// `heldTurnPatience`. The board draws a quiet queued mark for it and not the
	// `!`, which is kept for a message held against what its sender asked for.
	// Decided here and not on the board, so there is one rule. See heldPeer.quiet.
	HeldQuiet bool `json:"held_quiet,omitempty"`
	// LooksIdle says the card reads running but its terminal has gone quiet on an
	// idle prompt, so the turn-end never arrived. A guess, drawn as one.
	// IdleSeconds is how long the pty had been silent when it was decided. IN
	// MEMORY like the rest, cleared by any hook event, output or keystroke. See
	// looksidle.go.
	LooksIdle   bool  `json:"looks_idle,omitempty"`
	IdleSeconds int64 `json:"idle_seconds,omitempty"`
	// IdleAt is when it was flagged, which keys the board's alert so each firing
	// rings once.
	IdleAt time.Time `json:"idle_at,omitzero"`
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
	// held is the peer message waiting to be typed into each card's terminal,
	// kept apart from `by` because it does not expire on the activity clock. See
	// the HeldPeer note on Activity and `pendingInjector`.
	held map[string]heldPeer
	// background is how many subagents each card's last Stop said were still
	// running. See turnPaused.
	background map[string]int
	// bgWork is the non-subagent background tasks each card's last Stop said were
	// running. See stoppedSilently.
	bgWork map[string]bgHold
	// turns counts turns begun per card. See turnsBegun.
	turns map[string]int
	// busyAt is when a turn last began or a prompt last arrived, per card. See
	// sinceBusy.
	busyAt map[string]time.Time
	// looksIdle is each card flagged by the looks-idle watch. See looksidle.go.
	looksIdle map[string]idleMark
}

// heldPeer is a queued injection waiting on the operator's line to clear, on
// the turn to end, or on a dialog.
type heldPeer struct {
	from  string
	since time.Time
	why   string
	count int
	// turn is the HeldTurn constant when why is the turn.
	turn string
	// intended is every held message waiting for the turn, so nothing is held
	// against what its sender asked for. An immediate message queued behind one
	// that waits is not intended: it asked to go in now.
	intended bool
	// shownQuiet is what the board was last told, so the change from quiet to
	// overdue publishes once. See setHeld.
	shownQuiet bool
}

// heldTurnPatience is how long a message may wait for a turn before its mark
// turns from the quiet queued one to the `!`. A worker's turn runs for tens of
// minutes as a matter of course, so the bound is past that: a turn still going
// after an hour is worth a look, and a message waiting on one is not news before.
const heldTurnPatience = time.Hour

// quiet reports whether this hold is only the message waiting as it was meant
// to. See Activity.HeldQuiet.
func (h heldPeer) quiet(now time.Time) bool {
	return h.intended && h.why == HeldForTurn && now.Sub(h.since) < heldTurnPatience
}

// What is holding a message, as HeldFor says it. Three conditions and each is
// cleared by something different, which is why the board names the one that
// applies rather than blaming the line for all of them.
const (
	// HeldForLine is the operator's input line: text in it, or a keystroke in
	// the last `peerGateIdle`. Clearing or submitting the line lets it through.
	HeldForLine = "line"
	// HeldForTurn is the runner's turn. The message asked for `when: "done"`,
	// or the runner does not take input mid-turn. It goes when the turn ends.
	HeldForTurn = "turn"
	// HeldForDialog is a prompt the runner drew on its own screen, which an
	// Enter would answer. It goes once the dialog is answered.
	HeldForDialog = "dialog"
)

// Whose rule a turn wait is, as HeldTurn says it. The board names the one that
// applies rather than offering both.
const (
	// HeldTurnAsked is the sender's own `when: "done"`.
	HeldTurnAsked = "asked"
	// HeldTurnRunner is a runner that does not take input mid-turn, which holds
	// every message for the turn whatever its sender asked.
	HeldTurnRunner = "runner"
)

func newActivityTracker() *activityTracker {
	return &activityTracker{
		now:   time.Now,
		by:    map[string]*Activity{},
		tel:   map[string]*Telemetry{},
		telAt: map[string]time.Time{},
		held:  map[string]heldPeer{},

		background: map[string]int{},
		bgWork:     map[string]bgHold{},
		turns:      map[string]int{},
		busyAt:     map[string]time.Time{},
		looksIdle:  map[string]idleMark{},
	}
}

// setBackground records what a Stop said about subagents still running. Every
// Stop replaces it, so the one after the last report puts it back to zero.
func (a *activityTracker) setBackground(taskID string, n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n > 0 {
		a.background[taskID] = n
	} else {
		delete(a.background, taskID)
	}
}

// onSubagents reports whether the card's last Stop left subagents running.
//
// Read from the Stop and not from the SubagentStart and SubagentStop tally.
// Claude Code fires SubagentStop for more than the agents it started: a review
// panel of four raised four of them within a second, a minute before any
// reviewer had finished, and dozens more over the run.
func (a *activityTracker) onSubagents(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.background[taskID] > 0
}

// get returns a copy of a task's activity, or nil when no events have arrived
// or the last one is past the cutoff.
func (a *activityTracker) get(taskID string) *Activity {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.withLooksIdle(taskID, a.getLocked(taskID))
}

// getLocked is get with the lock held and no looks-idle mark attached.
func (a *activityTracker) getLocked(taskID string) *Activity {
	// A held peer message rides along whatever the running-process activity is
	// doing, and outlives its staleness. Computed first so it can be attached to
	// a fresh, a stale, or an absent activity all the same. See withHeld.
	cur := a.by[taskID]
	if cur == nil {
		return a.withHeld(taskID, nil)
	}
	age := a.now().Sub(cur.Since)
	if age > staleAfter {
		return a.withHeld(taskID, nil)
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
	return a.withHeld(taskID, &out)
}

// toolSince is the tool a card is running and when it started, READ PAST THE
// STALENESS CUTOFF. `get` stops believing a tool after `staleAfter`, which is
// right for the badge and exactly wrong for the watchdog: a tool call that has
// run that long is the one it is looking for. A session that died mid-tool is
// caught by the reaper and leaves the running set, so this does not report a
// dead process as stuck forever.
func (a *activityTracker) toolSince(taskID string) (string, time.Time, bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil || cur.What != ActivityTool {
		return "", time.Time{}, false
	}
	return cur.Tool, cur.Since, true
}

// withHeld attaches a held peer message to an activity, synthesising one when
// the card has no running-process activity to carry it. Caller holds the lock.
//
// The synthesised activity has no `What`, so a card that is only holding a
// message and doing nothing else reads as exactly that: nothing running, a
// message waiting. Returns the input untouched, possibly nil, when nothing is
// held, so the no-message path is what it always was.
func (a *activityTracker) withHeld(taskID string, out *Activity) *Activity {
	h, ok := a.held[taskID]
	if !ok {
		return out
	}
	if out == nil {
		out = &Activity{}
	}
	out.HeldPeer = h.from
	out.HeldSeconds = int64(a.now().Sub(h.since).Seconds())
	out.HeldFor = h.why
	out.HeldCount = h.count
	out.HeldTurn = h.turn
	out.HeldQuiet = h.quiet(a.now())
	return out
}

// setHeld records that a peer message is waiting to be typed into this card's
// terminal, keeping the first sender's clock so the age is how long the OLDEST
// held message has waited.
//
// The reason and the count in `r` are replaced on every call, because what
// holds the oldest message changes as the turn ends or the line clears, and the
// board says the current reason. See the HeldFor constants. Its `from` counts
// only for the first call, and its `since` is not read.
//
// Reports whether anything the board shows changed, so a retry every few
// seconds does not repaint it every few seconds. That includes a quiet hold
// passing `heldTurnPatience`, which no event marks: the retry that notices it
// publishes it.
func (a *activityTracker) setHeld(taskID string, r heldPeer) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.held[taskID]
	if !ok {
		h = heldPeer{from: r.from, since: a.now()}
	}
	changed := !ok || h.why != r.why || h.count != r.count || h.turn != r.turn || h.intended != r.intended
	h.why, h.count, h.turn, h.intended = r.why, r.count, r.turn, r.intended
	quiet := h.quiet(a.now())
	changed = changed || quiet != h.shownQuiet
	h.shownQuiet = quiet
	a.held[taskID] = h
	return changed
}

// clearHeld says nothing is waiting any more, because it landed, was delivered
// another way, or the terminal went away.
func (a *activityTracker) clearHeld(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	delete(a.held, taskID)
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
	if !midTurnState(cur.What) && midTurnState(what) {
		a.turns[taskID]++
		a.busyAt[taskID] = a.now()
	}
	cur.What, cur.Tool = what, tool
	// Any hook event is the runner speaking for itself, which settles the guess.
	delete(a.looksIdle, taskID)
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

// midTurn reports whether the runner is inside a turn: thinking, running a tool
// or compacting, with no Stop or Notification since. A peer message typed now
// would not submit. Claude Code holds a prompt that arrives mid-turn until the
// turn ends and then sends it together with whatever the operator typed in the
// meantime. See docs/typing-race.md.
//
// READ PAST THE STALENESS CUTOFF, like `toolSince`. A build that runs twenty
// minutes is still mid-turn, and typing into it is the race this guards. A card
// that has never posted activity is not mid-turn, so a runner with no hooks is
// typed into as before.
func (a *activityTracker) midTurn(taskID string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.by[taskID]
	if cur == nil {
		return false
	}
	return midTurnState(cur.What)
}

// midTurnState is whether an activity is one a turn is in.
func midTurnState(what string) bool {
	switch what {
	case ActivityThinking, ActivityTool, ActivityCompacting:
		return true
	}
	return false
}

// turnsBegun is how many times this card has gone from not working to working
// since the daemon started. A caller that types a prompt and must know whether
// a turn began reads it before and after, which sees a turn that was over
// between two polls. Never reset: a session ending forgets the activity, not
// this.
func (a *activityTracker) turnsBegun(taskID string) int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.turns[taskID]
}

// promptSeen stamps a prompt arriving. A prompt is the runner taking input, and
// it can land in a turn already counted, so `set` alone would miss it.
func (a *activityTracker) promptSeen(taskID string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.busyAt[taskID] = a.now()
}

// sinceBusy is how long since a turn last began or a prompt last arrived for this
// card, or a very long time when neither has. What a step that must not type into
// a prompt the runner is already taking asks, alongside `midTurn`: the hook for a
// prompt lands a moment after the Enter, and there is a gap before the turn shows.
func (a *activityTracker) sinceBusy(taskID string) time.Duration {
	a.mu.Lock()
	defer a.mu.Unlock()
	at, ok := a.busyAt[taskID]
	if !ok {
		return 1 << 62
	}
	return a.now().Sub(at)
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
	delete(a.held, taskID)
	delete(a.background, taskID)
	delete(a.looksIdle, taskID)
	delete(a.bgWork, taskID)
}

// bgHold is what a card's last Stop said about background work that is not a
// subagent: shells and the like. In memory on purpose, like everything here.
type bgHold struct {
	n     int
	since time.Time
}

// setBackgroundWork records how many non-subagent background tasks the last Stop
// left running. Every Stop replaces it, so the Stop that follows the last task's
// completion (which wakes the session) puts it back to zero.
func (a *activityTracker) setBackgroundWork(taskID string, n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if n > 0 {
		a.bgWork[taskID] = bgHold{n: n, since: a.now()}
	} else {
		delete(a.bgWork, taskID)
	}
}

// backgroundWork reports how many background tasks the card's last Stop left
// running, and since when. Zero once BackgroundHoldMax has passed, so a dev
// server left up on purpose holds the alert for a while and not forever.
func (a *activityTracker) backgroundWork(taskID string) (int, time.Time) {
	a.mu.Lock()
	defer a.mu.Unlock()
	h, ok := a.bgWork[taskID]
	if !ok || a.now().Sub(h.since) >= BackgroundHoldMax {
		return 0, time.Time{}
	}
	return h.n, h.since
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

	// The runner spoke for itself, which settles a looks-idle guess. Logged here
	// because `set` clears it silently.
	d.looksIdleGone(taskID, in.Agent, "hook "+in.Event)

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
		d.act.promptSeen(taskID)
		d.turnResumed(taskID)
		// What started this turn, for the usage record. See usage.go.
		cause := d.promptCause(taskID)
		d.usage.prompted(taskID, cause)
		if cause == store.UsageOperator {
			d.humanTouch(taskID, ViaPrompt)
		}
		// And it saw the turn and answered its questions, unless the prompt
		// was a peer's message atrium typed in. See seen.go.
		d.seenPrompted(taskID)
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
		// "Claude is waiting for your input" says only that the prompt has sat
		// idle, and a session whose subagents are still out is idle on purpose.
		// A question or a dialog still counts. See turnPaused.
		if in.Event == "waiting" && in.Notification == "idle_prompt" && d.act.onSubagents(taskID) {
			break
		}
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
