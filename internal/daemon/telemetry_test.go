package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// waitForTelemetry polls, because /telemetry answers before it records.
func waitForTelemetry(t *testing.T, d *Daemon, taskID string) *Telemetry {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c := d.act.telemetry(taskID); c != nil {
			return c
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("no telemetry ever arrived for %s", taskID)
	return nil
}

// The join the whole feature rests on. A statusline is handed the harness's
// session id and nothing else, and atrium already stores that id as the card's
// resume id, so the lookup has to work without the caller knowing a wire name
// or a card id.
func TestTelemetryLandsOnACardByItsSessionID(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "tel-test", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(task.ID, "sess-abc-123"); err != nil {
		t.Fatal(err)
	}

	url := "http://" + d.opts.AgentAddr + "/telemetry"
	resp := postJSON(t, url, TelemetryEvent{
		SessionID: "sess-abc-123", ContextUsed: 184000, ContextWindow: 200000,
		Model: "Opus 5",
	})
	resp.Body.Close()

	got := waitForTelemetry(t, d, task.ID)
	if got.Pct != 92 {
		t.Fatalf("context is %d%%, wanted 92: 184000 of 200000", got.Pct)
	}
	if got.Used != 184000 || got.Window != 200000 {
		t.Fatalf("tokens are %d/%d, wanted 184000/200000", got.Used, got.Window)
	}
	if got.Model != "Opus 5" {
		t.Fatalf("model is %q, wanted Opus 5", got.Model)
	}
}

// A session id that matches no card records nothing and still answers ok.
//
// A statusline renders in every session on the machine, including ones atrium
// has never heard of. Failing there would put an error in somebody's prompt
// several times a second.
func TestTelemetryForAnUnknownSessionIsAcceptedAndDropped(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	url := "http://" + d.opts.AgentAddr + "/telemetry"
	resp := postJSON(t, url, TelemetryEvent{
		SessionID: "nobody-has-this-id", ContextUsed: 100, ContextWindow: 200,
	})
	code := resp.StatusCode
	resp.Body.Close()
	if code != http.StatusOK {
		t.Fatalf("an unknown session got %d, which its statusline would have to handle", code)
	}
	if got := d.onTelemetry(TelemetryEvent{SessionID: "nobody-has-this-id", ContextPct: 50}); got != "" {
		t.Fatalf("an unknown session landed on task %q", got)
	}
}

// An empty session id must not match the first card with a blank resume id.
//
// Most cards have one: not every runner reports an id, and SetResumeID ignores
// a blank rather than storing it. A caller that omitted the field would
// otherwise have its figure drawn on somebody else's card, which is worse than
// no figure at all.
func TestTelemetryWithNoKeyMatchesNothing(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	if _, _, err := d.st.Register(store.Observed{
		WireName: "no-resume-id", Worktree: "/tmp/atrium-test", Runner: "claude",
	}); err != nil {
		t.Fatal(err)
	}

	if got := d.onTelemetry(TelemetryEvent{ContextPct: 90}); got != "" {
		t.Fatalf("a post with no key landed on task %q", got)
	}
	if got := d.taskForTelemetry(TelemetryEvent{SessionID: "   "}); got != "" {
		t.Fatalf("a blank session id resolved to task %q", got)
	}
}

// Nothing a telemetry post contains can produce a non-2xx, including garbage.
//
// The same rule /activity follows, and it matters more here: a statusline runs
// on every redraw, so a caller that logged a failure would log it forever.
func TestTelemetryEndpointNeverRefuses(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	url := "http://" + d.opts.AgentAddr + "/telemetry"
	for _, body := range []string{
		`not json at all`,
		`{"session_id":`,
		``,
		`{"session_id":123,"context_used":"lots"}`,
		`[]`,
		`{"context_used":-5,"context_window":-1}`,
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

// A percentage outside 0..100 is drawn as a chip, so it is clamped rather than
// trusted. A caller that sends more used than window is wrong, and a card
// reading "ctx 340%" is a board nobody believes again.
func TestTelemetryClampsWhatItIsGiven(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := mustCardWithResume(t, d, "clamp-test", "sess-clamp")

	d.onTelemetry(TelemetryEvent{
		SessionID: "sess-clamp", ContextUsed: 600000, ContextWindow: 200000,
		FiveHour: &LimitEvent{Pct: 900},
	})
	got := d.act.telemetry(task.ID)
	if got == nil {
		t.Fatal("nothing recorded")
	}
	if got.Pct != 100 {
		t.Fatalf("context is %d%%, wanted it clamped to 100", got.Pct)
	}
	if got.FiveHour == nil || got.FiveHour.Pct != 100 {
		t.Fatalf("five hour limit is %v, wanted it clamped to 100", got.FiveHour)
	}
}

// A reset time that will not parse costs the timestamp and not the limit.
//
// The percentage is the part anybody acts on, and the two sides of this
// contract are written in different languages by different people. Throwing
// away "the account is at 94% of its weekly limit" over a formatting
// disagreement is the wrong trade.
func TestABadResetTimeKeepsThePercentage(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := mustCardWithResume(t, d, "reset-test", "sess-reset")

	d.onTelemetry(TelemetryEvent{
		SessionID:  "sess-reset",
		ContextPct: 40,
		Weekly:     &LimitEvent{Pct: 94, ResetsAt: "next tuesday"},
	})
	got := d.act.telemetry(task.ID)
	if got == nil || got.Weekly == nil {
		t.Fatal("an unparseable reset time threw away the whole limit")
	}
	if got.Weekly.Pct != 94 {
		t.Fatalf("weekly limit is %d%%, wanted 94", got.Weekly.Pct)
	}
	if !got.Weekly.ResetsAt.IsZero() {
		t.Fatalf("an unparseable reset time became %v", got.Weekly.ResetsAt)
	}
}

// A statusline redraws when the TERMINAL does, which includes a human typing
// in it and a timer firing with nothing happening at all.
//
// Counting that as the agent doing something would make the board's quietest
// card be "the one nobody has looked at" rather than "the one that stopped",
// which is the sort the whole dispatch decision runs on.
func TestTelemetryIsNotActivity(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := mustCardWithResume(t, d, "quiet-test", "sess-quiet")
	before, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}

	d.onTelemetry(TelemetryEvent{SessionID: "sess-quiet", ContextPct: 71})

	after, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !after.LastActivityAt.Equal(before.LastActivityAt) {
		t.Fatalf("a statusline post moved last activity from %v to %v",
			before.LastActivityAt, after.LastActivityAt)
	}
	if a := d.act.get(task.ID); a != nil && a.What != "" {
		t.Fatalf("a statusline post set the activity badge to %q", a.What)
	}
}

// The contract asks for one post every ten to fifteen seconds. This is what
// stops a caller that ignores it from turning every terminal redraw into a
// database read.
func TestTelemetryFloorDropsAFloodOfPosts(t *testing.T) {
	at, clock := fixedClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	tr := newActivityTracker()
	tr.now = clock

	if !tr.telemetryAllowed("sess-1") {
		t.Fatal("the first post was dropped")
	}
	for i := 0; i < 50; i++ {
		if tr.telemetryAllowed("sess-1") {
			t.Fatal("a post inside the floor was accepted")
		}
	}
	// Another session is another caller, and one noisy statusline must not
	// silence the rest of the board.
	if !tr.telemetryAllowed("sess-2") {
		t.Fatal("a different session was dropped by the first one's floor")
	}

	*at = at.Add(telemetryFloor)
	if !tr.telemetryAllowed("sess-1") {
		t.Fatal("a post past the floor was still dropped")
	}
}

// A figure expires on its own, so a statusline that stops posting stops
// claiming a number. It lives LONGER than an activity does, because context
// only grows: a figure from ten minutes ago is a floor on the figure now.
func TestTelemetryGoesStaleOnItsOwnClock(t *testing.T) {
	at, clock := fixedClock(time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC))
	tr := newActivityTracker()
	tr.now = clock

	tr.setTelemetry("t1", Telemetry{Pct: 88})
	tr.set("t1", ActivityTool, "Bash")

	// Past the activity cutoff and inside the telemetry one: the badge is gone
	// and the number is still there, which is the case this separation exists
	// for. An idle card is exactly the one whose context decides whether you
	// resume it.
	*at = at.Add(staleAfter + time.Minute)
	if tr.get("t1") != nil {
		t.Fatal("a stale activity was still believed")
	}
	if c := tr.telemetry("t1"); c == nil || c.Pct != 88 {
		t.Fatal("the context figure expired with the activity badge")
	}

	*at = at.Add(telemetryStaleAfter)
	if c := tr.telemetry("t1"); c != nil {
		t.Fatalf("a figure %v old was still believed", telemetryStaleAfter)
	}
}

// A session ending takes its context figure with it. The number described a
// conversation that is over, and it is the number that decides which card you
// go and look at.
func TestForgettingACardDropsItsTelemetry(t *testing.T) {
	tr := newActivityTracker()
	tr.setTelemetry("t1", Telemetry{Pct: 92})
	tr.set("t1", ActivityTool, "Bash")

	tr.forget("t1")

	if c := tr.telemetry("t1"); c != nil {
		t.Fatalf("a forgotten card still reports %d%% context", c.Pct)
	}
	if a := tr.get("t1"); a != nil {
		t.Fatal("a forgotten card still reports an activity")
	}
}

// An empty post must not blank a good figure. A statusline that could not work
// any of this out should send nothing, and one that sends an empty body anyway
// would otherwise replace a real number with a chip that says nothing.
func TestAnEmptyTelemetryPostIsNotRecorded(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task := mustCardWithResume(t, d, "empty-test", "sess-empty")

	d.onTelemetry(TelemetryEvent{SessionID: "sess-empty", ContextPct: 77})
	// Past the floor, so the second post is judged on its content and not on
	// its timing.
	d.act.mu.Lock()
	delete(d.act.telAt, "sess-empty")
	d.act.mu.Unlock()
	d.onTelemetry(TelemetryEvent{SessionID: "sess-empty", Model: "Opus 5"})

	got := d.act.telemetry(task.ID)
	if got == nil || got.Pct != 77 {
		t.Fatalf("an empty post overwrote a good figure: %v", got)
	}
}

func mustCardWithResume(t *testing.T, d *Daemon, wire, resume string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{
		WireName: wire, Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(task.ID, resume); err != nil {
		t.Fatal(err)
	}
	return task
}
