package daemon

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func fixedClock(start time.Time) (*time.Time, func() time.Time) {
	at := start
	return &at, func() time.Time { return at }
}

// A card in running looks the same whether its session is grinding through a
// build or hung. The activity is what tells those apart, so the tool name and
// the age both have to survive the round trip.
func TestActivityReportsToolAndAge(t *testing.T) {
	at, clock := fixedClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	tr := newActivityTracker()
	tr.now = clock

	tr.set("t1", ActivityTool, "Bash")
	*at = at.Add(90 * time.Second)

	got := tr.get("t1")
	if got == nil {
		t.Fatal("no activity for a task that just started a tool")
	}
	if got.What != ActivityTool || got.Tool != "Bash" {
		t.Fatalf("activity is %s/%s, wanted tool/Bash", got.What, got.Tool)
	}
	if got.Seconds != 90 {
		t.Fatalf("age is %ds, wanted 90", got.Seconds)
	}
}

// An unchanged state keeps counting from when it began. Otherwise every hook
// would reset the clock and "thinking for 20 minutes" could never be shown.
func TestActivityKeepsItsStartTimeWhenUnchanged(t *testing.T) {
	at, clock := fixedClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	tr := newActivityTracker()
	tr.now = clock

	tr.set("t1", ActivityThinking, "")
	*at = at.Add(time.Minute)
	tr.set("t1", ActivityThinking, "")
	*at = at.Add(time.Minute)

	got := tr.get("t1")
	if got.Seconds != 120 {
		t.Fatalf("age is %ds, wanted 120: the clock restarted on an unchanged state", got.Seconds)
	}
}

// A different tool is a different activity, so its clock restarts.
func TestActivityRestartsOnAChange(t *testing.T) {
	at, clock := fixedClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	tr := newActivityTracker()
	tr.now = clock

	tr.set("t1", ActivityTool, "Bash")
	*at = at.Add(time.Minute)
	tr.set("t1", ActivityTool, "Read")
	*at = at.Add(10 * time.Second)

	got := tr.get("t1")
	if got.Seconds != 10 {
		t.Fatalf("age is %ds, wanted 10: a new tool did not restart the clock", got.Seconds)
	}
}

// Hooks stop arriving without warning, a session killed mid-tool being the
// obvious case. Past the cutoff the honest answer is that atrium does not know,
// not that a long-dead process is still running Bash.
func TestActivityGoesQuietWhenStale(t *testing.T) {
	at, clock := fixedClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	tr := newActivityTracker()
	tr.now = clock

	tr.set("t1", ActivityTool, "Bash")
	*at = at.Add(staleAfter - time.Second)
	if tr.get("t1") == nil {
		t.Fatal("activity was written off before the cutoff")
	}
	*at = at.Add(2 * time.Second)
	if got := tr.get("t1"); got != nil {
		t.Fatalf("a stale activity is still being reported as %s/%s", got.What, got.Tool)
	}
}

// Every hook is best effort, so a dropped SubagentStart puts the count out of
// step. A negative count would render as nonsense, so the floor is enforced
// rather than assumed.
func TestSubagentCountNeverGoesNegative(t *testing.T) {
	tr := newActivityTracker()
	tr.set("t1", ActivityThinking, "")

	tr.addSubagents("t1", -1)
	tr.addSubagents("t1", -1)
	if got := tr.get("t1"); got.Subagents != 0 {
		t.Fatalf("subagent count is %d, wanted 0", got.Subagents)
	}
	tr.addSubagents("t1", 1)
	if got := tr.get("t1"); got.Subagents != 1 {
		t.Fatalf("subagent count is %d after one start, wanted 1", got.Subagents)
	}
}

// The count is a running tally, not part of the state, so changing what a
// session is doing must not lose track of its subagents.
func TestSubagentCountSurvivesAStateChange(t *testing.T) {
	tr := newActivityTracker()
	tr.addSubagents("t1", 2)
	tr.set("t1", ActivityTool, "Bash")

	if got := tr.get("t1"); got.Subagents != 2 {
		t.Fatalf("subagent count is %d after a state change, wanted 2", got.Subagents)
	}
}

func postJSON(t *testing.T, url string, body any) *http.Response {
	t.Helper()
	blob, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(blob))
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	return resp
}

// waitForActivity polls until a task has one, because /activity answers before
// it records.
func waitForActivity(t *testing.T, d *Daemon, taskID string) *Activity {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a := d.act.get(taskID); a != nil && a.What != "" {
			return a
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no activity ever arrived for %s", taskID)
	return nil
}

// The whole point is that a session with no card, or one atrium has never
// heard of, must not fail. This rides PreToolUse, so anything that errors here
// breaks every tool call a session makes.
func TestActivityEndpointAcceptsAnUnknownAgent(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	url := "http://" + d.opts.AgentAddr + "/activity"
	resp := postJSON(t, url, ActivityEvent{Agent: "nobody-here", Event: "tool-start", Tool: "Bash"})
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("an unknown agent got %s, which would break its tool calls", resp.Status)
	}
}

// Nothing an activity post contains can produce a non-2xx, including garbage.
//
// A caller that treats non-2xx as a failure would surface or retry it, and this
// runs before every tool call in every session. The contract is that activity
// reporting can never affect a tool call, and a 400 on a malformed body breaks
// that just as surely as a slow answer would.
func TestActivityEndpointNeverRefuses(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	url := "http://" + d.opts.AgentAddr + "/activity"
	for _, body := range []string{
		`not json at all`,
		`{"agent":`,
		``,
		`{"agent":123,"event":true}`,
		`[]`,
	} {
		resp, err := http.Post(url, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatalf("post %q: %v", body, err)
		}
		code := resp.StatusCode
		resp.Body.Close()
		if code != http.StatusOK {
			t.Errorf("body %q got %d, which a caller would treat as a failure", body, code)
		}
	}
}

// End to end: a hook posts, the card carries the activity.
func TestActivityEndpointRecordsAgainstACard(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "act-test", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	url := "http://" + d.opts.AgentAddr + "/activity"
	resp := postJSON(t, url, ActivityEvent{Agent: "act-test", Event: "tool-start", Tool: "Bash"})
	resp.Body.Close()

	got := waitForActivity(t, d, task.ID)
	if got.What != ActivityTool || got.Tool != "Bash" {
		t.Fatalf("card shows %s/%s, wanted tool/Bash", got.What, got.Tool)
	}

	// A tool ending hands the floor back to the model.
	resp = postJSON(t, url, ActivityEvent{Agent: "act-test", Event: "tool-end"})
	resp.Body.Close()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a := d.act.get(task.ID); a != nil && a.What == ActivityThinking {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("a finished tool left the card showing %v", d.act.get(task.ID))
}

// SubagentStart takes the count up and SubagentStop takes it down. A Task tool
// call raises the same subagent, so counting that as well would report double
// what is running.
func TestSubagentCountFollowsTheSubagentHooks(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "sub-test", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	url := "http://" + d.opts.AgentAddr + "/activity"
	// Both halves of what one subagent actually posts: the Task call that
	// spawned it, and the SubagentStart that reports it.
	for i := 0; i < 2; i++ {
		resp := postJSON(t, url, ActivityEvent{
			Agent: "sub-test", Event: "tool-start", Tool: "Task",
		})
		resp.Body.Close()
		resp = postJSON(t, url, ActivityEvent{
			Agent: "sub-test", Event: "subagent-start",
		})
		resp.Body.Close()
	}
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a := d.act.get(task.ID); a != nil && a.Subagents == 2 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if a := d.act.get(task.ID); a == nil || a.Subagents != 2 {
		t.Fatalf("two subagents produced %v, wanted 2", a)
	}

	resp := postJSON(t, url, ActivityEvent{Agent: "sub-test", Event: "subagent-end"})
	resp.Body.Close()
	deadline = time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if a := d.act.get(task.ID); a != nil && a.Subagents == 1 {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("a subagent ending left the count at %v", d.act.get(task.ID))
}

// A session ending has to take its activity with it, or its card claims to be
// running a tool inside a process that has exited.
func TestSessionEndClearsActivity(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "end-test", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	d.act.set(task.ID, ActivityTool, "Bash")

	if err := d.onSession(SessionEvent{Agent: "end-test", Event: "end"}); err != nil {
		t.Fatalf("session end: %v", err)
	}
	if a := d.act.get(task.ID); a != nil {
		t.Fatalf("a card for an exited session still shows %s/%s", a.What, a.Tool)
	}
}

// The board reads activity off the task view, so it has to be there. An absent
// one has to be absent rather than an empty object, or every card would render
// a blank badge.
func TestActivityReachesTheBoardView(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "view-test", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}

	base := "http://" + d.opts.HumanAddr
	fetch := func() map[string]any {
		resp, err := http.Get(fmt.Sprintf("%s/v1/tasks/%s", base, task.ID))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		return out
	}

	if _, present := fetch()["activity"]; present {
		t.Fatal("a card with no activity carries an activity field anyway")
	}

	d.act.set(task.ID, ActivityTool, "Bash")
	got := fetch()
	a, present := got["activity"].(map[string]any)
	if !present {
		t.Fatalf("activity never reached the view: %v", got)
	}
	if a["what"] != ActivityTool || a["tool"] != "Bash" {
		t.Fatalf("view shows %v, wanted tool/Bash", a)
	}
}
