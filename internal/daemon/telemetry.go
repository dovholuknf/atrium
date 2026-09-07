package daemon

import (
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

// How much context a session has burned, and how close its account is to a
// limit.
//
// The one thing about a running agent that atrium could not see. Hooks carry
// which tool started and which subagent ended, and none of them carry the
// number that decides which of sixteen agents you interrupt: the one at ninety
// percent of its window is about to compact, and compacting is where a session
// forgets what it was told.
//
// Claude Code hands its statusline script the richest per-session payload it
// exposes, and the statusline is the only thing on the machine that gets it.
// So the statusline posts it here.
//
// The statusline script itself lives outside this repository. What atrium owns
// is this endpoint, what the board draws from it, and the contract in
// `docs/statusline-telemetry.md`, which is written to be implementable without
// asking anybody a question.
//
// NEVER STORED, for the reason in `docs/activity-design.md`. A context figure
// is a fact about a process that is running right now. Written down, it
// survives the restart that killed the session it described and becomes a
// confident lie about which agent needs you.

// telemetryStaleAfter is how long a context figure is believed.
//
// Longer than staleAfter, which governs the activity badge, and the difference
// is deliberate. An activity goes wrong quickly: "running Bash" is false the
// moment the tool ends, and a killed session pins it forever. A context figure
// goes wrong slowly, because context only grows: a number from ten minutes ago
// is a floor on the number now, which is exactly what the decision to
// interrupt wants.
//
// It still expires. A statusline stops posting when its session ends, and half
// an hour later the conversation that number described has probably been
// replaced by another one in the same terminal.
const telemetryStaleAfter = 30 * time.Minute

// telemetryFloor is the shortest gap between two accepted posts for one
// session.
//
// The contract asks the caller to post every ten to fifteen seconds rather
// than on every render, and a statusline renders many times a second. This is
// what happens when a caller does not: the post is dropped before the store is
// touched, so a misbehaving statusline costs one map lookup and not one
// database read per keystroke.
//
// Well under the documented cadence on purpose. A caller obeying the contract
// is never dropped by this, so it cannot become a rate the caller has to tune
// against.
const telemetryFloor = 2 * time.Second

// Limit is one usage limit and when it resets.
//
// Percentages rather than token counts, because that is what the payload
// carries and because "eighty percent of the week" is the number a human acts
// on. ResetsAt is optional: a limit with no reset time still says how close
// the account is.
type Limit struct {
	Pct      int       `json:"pct"`
	ResetsAt time.Time `json:"resets_at,omitempty"`
}

// Telemetry is one session's context and limit picture.
type Telemetry struct {
	// Pct is context used as a percentage of the window, always filled in.
	// Derived from Used and Window when both are given, since a caller that
	// sends all three can contradict itself and the pair is the better source.
	Pct int `json:"pct"`
	// Used and Window are the tokens behind it, when the caller knows them.
	// Shown in the tooltip, so "92%" can be read as a size rather than only as
	// a fraction.
	Used   int `json:"used,omitempty"`
	Window int `json:"window,omitempty"`
	// Model is what the session is running, as the harness displays it. Worth
	// having beside a percentage: the same percentage is a different amount of
	// remaining room on a different window.
	Model string `json:"model,omitempty"`
	// FiveHour and Weekly are the account's rolling limits. Absent when the
	// caller does not report them, which is the normal case for a plan that
	// has none.
	FiveHour *Limit `json:"five_hour,omitempty"`
	Weekly   *Limit `json:"weekly,omitempty"`
	// Since is when this figure arrived, so the board can age it and so a
	// stale one can be dropped.
	Since time.Time `json:"since"`
	// Seconds is Since as an age, filled in when the telemetry is served.
	Seconds int64 `json:"seconds"`
}

// TelemetryEvent is what a statusline posts to /telemetry.
//
// Deliberately not Claude Code's statusline payload passed through. That
// payload is the harness's to change, it carries a great deal atrium has no
// business holding, and a contract shaped like somebody else's internal JSON
// is one that breaks on their release schedule. The caller extracts, atrium
// receives named fields.
type TelemetryEvent struct {
	// SessionID is the harness's own id for the conversation, which atrium
	// already stores as a card's resume id. The primary key, because it is the
	// one identifier a statusline definitely has.
	SessionID string `json:"session_id,omitempty"`
	// TaskID is the card outright, for a caller that was told which one it is.
	// Beats every other key when present.
	TaskID string `json:"task_id,omitempty"`
	// Agent is the wire name, the fallback every other hook uses.
	Agent string `json:"agent,omitempty"`

	// ContextUsed and ContextWindow are tokens. Preferred over ContextPct.
	ContextUsed   int `json:"context_used,omitempty"`
	ContextWindow int `json:"context_window,omitempty"`
	// ContextPct is for a caller that only knows the fraction.
	ContextPct int `json:"context_pct,omitempty"`

	Model string `json:"model,omitempty"`

	// FiveHour and Weekly carry a percentage and an RFC3339 reset time. A
	// reset time that will not parse is dropped rather than refused: the
	// percentage is the part that decides anything.
	FiveHour *LimitEvent `json:"five_hour,omitempty"`
	Weekly   *LimitEvent `json:"weekly,omitempty"`
}

// LimitEvent is one limit on the wire.
type LimitEvent struct {
	Pct      int    `json:"pct"`
	ResetsAt string `json:"resets_at,omitempty"`
}

// handleTelemetry records how much context a session has used.
//
// Follows /activity line for line, because it is the same kind of caller with
// a worse duty cycle: a statusline renders many times a second and a hook runs
// once per tool call. It answers before it does any work, it swallows a
// session nobody has heard of, and no failure of any kind reaches the caller.
//
// A statusline that stalls is a terminal that stops drawing, so the thing this
// must never do is make anybody wait.
func (d *Daemon) handleTelemetry(w http.ResponseWriter, r *http.Request) {
	// Answered before the body is read, exactly as /activity is, and for the
	// same reason: nothing here can produce a non-2xx, because a caller that
	// treats non-2xx as a failure would log or retry it once per render.
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"ok":true}`))

	var in TelemetryEvent
	if err := json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(&in); err != nil {
		log.Printf("[atrium] unreadable telemetry post: %v", err)
		return
	}

	go d.onTelemetry(in)
}

// onTelemetry applies one post. Returns the task it landed on, or "" when
// there was nothing to land on and when the post was dropped by the floor.
func (d *Daemon) onTelemetry(in TelemetryEvent) string {
	key := telemetryKey(in)
	if key == "" {
		return ""
	}
	// The floor comes first, ahead of the store lookup, because the whole
	// point of it is to be cheaper than a store lookup.
	if !d.act.telemetryAllowed(key) {
		return ""
	}

	taskID := d.taskForTelemetry(in)
	if taskID == "" {
		// A session with no card. Answered `ok` already, and nothing recorded.
		// Not logged: this runs on a timer forever in every session, so a
		// statusline pointed at a daemon that has never heard of it would fill
		// the log with one line every fifteen seconds.
		return ""
	}

	t := Telemetry{
		Used:   clampInt(in.ContextUsed, 0, 1<<30),
		Window: clampInt(in.ContextWindow, 0, 1<<30),
		Model:  trimTo(in.Model, 60),
	}
	switch {
	case t.Window > 0:
		// The pair wins over a percentage the caller also sent. A caller can
		// contradict itself and the tokens are the thing the tooltip shows, so
		// the two must agree.
		t.Pct = clampInt(t.Used*100/t.Window, 0, 100)
	default:
		t.Pct = clampInt(in.ContextPct, 0, 100)
	}
	t.FiveHour = toLimit(in.FiveHour)
	t.Weekly = toLimit(in.Weekly)

	// A post with nothing in it is not a figure. A statusline that could not
	// work out any of this should send nothing, and if it sends an empty one
	// anyway, recording it would replace a good figure with a blank chip.
	if t.Pct == 0 && t.Used == 0 && t.FiveHour == nil && t.Weekly == nil {
		return ""
	}

	// NOT an activity report, and not a touch of the card's last activity.
	//
	// A statusline redraws when the TERMINAL does, which includes a human
	// typing in it, resizing the window, or nothing at all happening on a
	// timer. Counting that as the agent doing something would make "quietest
	// card" mean "card nobody has looked at", and the whole point of that sort
	// is to find the session that stopped.
	d.act.setTelemetry(taskID, t)
	return taskID
}

// telemetryKey is what the floor debounces on: whatever the caller used to
// identify itself, before any of it is resolved to a card.
func telemetryKey(in TelemetryEvent) string {
	for _, s := range []string{in.TaskID, in.SessionID, in.Agent} {
		if s = strings.TrimSpace(s); s != "" {
			return s
		}
	}
	return ""
}

// taskForTelemetry resolves a post to a card, or "".
//
// Order matters and is documented in `docs/statusline-telemetry.md`: the card
// id outright, then the harness's session id against the resume id atrium
// already records, then the wire name every other hook uses. A statusline is
// handed the session id and nothing else, so the middle one is the path that
// carries the traffic.
func (d *Daemon) taskForTelemetry(in TelemetryEvent) string {
	if id := strings.TrimSpace(in.TaskID); id != "" {
		if t, err := d.st.Get(id); err == nil && t != nil {
			return t.ID
		}
	}
	if sid := strings.TrimSpace(in.SessionID); sid != "" {
		if t, err := d.st.GetByResumeID(sid); err == nil && t != nil {
			return t.ID
		}
	}
	if name := strings.TrimSpace(in.Agent); name != "" {
		if t, err := d.st.GetByWireName(name); err == nil && t != nil {
			return t.ID
		}
	}
	return ""
}

// toLimit converts one wire limit, dropping a reset time that will not parse.
//
// A limit with an unreadable timestamp is still a limit, and refusing the
// whole thing over a formatting disagreement would throw away the percentage,
// which is the part anybody acts on.
func toLimit(in *LimitEvent) *Limit {
	if in == nil {
		return nil
	}
	out := &Limit{Pct: clampInt(in.Pct, 0, 100)}
	if s := strings.TrimSpace(in.ResetsAt); s != "" {
		if at, err := time.Parse(time.RFC3339, s); err == nil {
			out.ResetsAt = at
		}
	}
	return out
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// trimTo bounds a string the caller chose. Nothing here is trusted to be
// short, and a model name is drawn on a card.
func trimTo(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n]
	}
	return s
}

// setTelemetry replaces a task's figure.
//
// Kept beside the activity map rather than in the Activity struct, because the
// two expire on different clocks and for different reasons. Folding them
// together would mean a session idle for twenty minutes lost its context
// figure along with its stale "running Bash", and the idle one is exactly the
// card you are deciding whether to resume.
func (a *activityTracker) setTelemetry(taskID string, t Telemetry) {
	a.mu.Lock()
	defer a.mu.Unlock()
	t.Since = a.now()
	a.tel[taskID] = &t
}

// telemetry returns a copy of a task's figure, or nil when none has arrived or
// the last one is past the cutoff.
func (a *activityTracker) telemetry(taskID string) *Telemetry {
	a.mu.Lock()
	defer a.mu.Unlock()
	cur := a.tel[taskID]
	if cur == nil {
		return nil
	}
	age := a.now().Sub(cur.Since)
	if age > telemetryStaleAfter {
		return nil
	}
	out := *cur
	out.Seconds = int64(age.Seconds())
	return &out
}

// telemetryAllowed reports whether enough time has passed since this caller's
// last accepted post, and records the attempt when it has.
func (a *activityTracker) telemetryAllowed(key string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	n := a.now()
	if last, ok := a.telAt[key]; ok && n.Sub(last) < telemetryFloor {
		return false
	}
	a.telAt[key] = n
	// Keyed by what the CALLER said it was, which is not a card id, so `forget`
	// cannot clean it up. Left alone it grows by one entry per session the
	// daemon ever hears from, forever. Swept only when it is big enough to be
	// worth sweeping, so the common path stays one map write.
	if len(a.telAt) > 512 {
		for k, at := range a.telAt {
			if n.Sub(at) > telemetryStaleAfter {
				delete(a.telAt, k)
			}
		}
	}
	return true
}

// TelemetryFor adapts the tracker to what the api package expects. An absent
// figure returns an untyped nil, since a typed nil through an interface
// serialises as a present-but-empty object.
func (d *Daemon) telemetryFor(taskID string) any {
	t := d.act.telemetry(taskID)
	if t == nil {
		return nil
	}
	return t
}
