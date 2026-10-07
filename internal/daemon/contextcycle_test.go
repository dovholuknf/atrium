package daemon

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// The context cycle: the card passes its limit, atrium asks for a handoff once per
// turn until `atrium ready`, then clears and wakes it. See docs/runtime/context-cycle-design.md.

// cycleCard is a card atrium supervises, a person's own (no launcher), with a
// transcript at the given size.
func cycleCard(t *testing.T, d *Daemon, tokens int64) (*store.Task, *fakePTY, func(int64)) {
	t.Helper()
	task, f, _ := ncCard(t, d)
	reply := withTranscript(t, d, task)
	reply(tokens)
	task, err := d.st.Get(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	return task, f, reply
}

// midTurn puts the card inside a turn, with a screen that shows one.
func midTurn(d *Daemon, id string) {
	_ = d.st.SetStatus(id, store.StatusRunning)
	d.act.set(id, ActivityThinking, "")
	if run := d.sup.get(id); run != nil {
		_, _ = run.buf.Write([]byte(workingFrame))
		run.lastOut.Store(time.Now().UnixNano())
	}
}

func prompts(f *fakePTY) int { return strings.Count(f.written(), limitPrompt) }

// statuslines is a burst of statusline updates, each one re-checking the card.
func statuslines(d *Daemon, id string, n int) {
	for i := 0; i < n; i++ {
		d.cycleOnStatusline(id)
		time.Sleep(5 * time.Millisecond)
	}
}

func step(d *Daemon, id string) string {
	v, _ := d.newContextFor(id).(map[string]any)
	if v == nil {
		return ""
	}
	s, _ := v["step"].(string)
	return s
}

// Plan 5 and 7: past the limit mid-turn, the prompt is typed at once, and then again
// once per turn however many statusline updates arrive inside one. Never cleared
// without the ack.
func TestTheLimitMidTurnTypesThePromptOncePerTurn(t *testing.T) {
	fastNewContext(t)
	ncTiming.promptLost = time.Hour
	d := testDaemon(t)
	task, f, _ := cycleCard(t, d, 250_000)
	id := task.ID
	midTurn(d, id)

	statuslines(d, id, 1)
	until(t, "the limit prompt mid-turn", func() bool { return prompts(f) == 1 && strings.HasSuffix(f.written(), "\r") })
	if v, _ := d.newContextFor(id).(map[string]any); v == nil || v["auto"] != true || v["step"] != NewContextLimit {
		t.Fatalf("no automatic cycle on the limit step: %v", d.newContextFor(id))
	}

	for turn := 1; turn <= 3; turn++ {
		// A burst inside the turn types nothing more.
		statuslines(d, id, 20)
		if got := prompts(f); got != turn {
			t.Fatalf("turn %d: %d prompts after a burst of statusline updates, want %d", turn, got, turn)
		}
		// The turn ends with no ack: it is typed again, once.
		ncTurnEnds(d, id)
		until(t, "the prompt again after the turn", func() bool {
			return prompts(f) == turn+1 && strings.HasSuffix(f.written(), "\r")
		})
		statuslines(d, id, 20)
		if got := prompts(f); got != turn+1 {
			t.Fatalf("turn %d: %d prompts between turns, want %d", turn, got, turn+1)
		}
		// The prompt starts the next turn.
		midTurn(d, id)
	}
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("cleared with no ack: %q", f.written())
	}
	if step(d, id) != NewContextLimit {
		t.Fatalf("the cycle left the limit step with no ack: %v", d.newContextFor(id))
	}
	if v, _ := d.newContextFor(id).(map[string]any); v["prompted"] != 4 {
		t.Fatalf("the chip counts %v prompts, want 4", v["prompted"])
	}
}

// Plan 7: no ack, no clear, for longer than any other step would wait.
func TestNoAckNeverClears(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := cycleCard(t, d, 300_000)
	id := task.ID
	if err := os.WriteFile(d.handoffPath(task), handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	statuslines(d, id, 1)
	until(t, "the limit prompt", func() bool { return prompts(f) == 1 })
	deadline := time.Now().Add(ncTiming.captureEnd + ncTiming.clearWait)
	for time.Now().Before(deadline) {
		midTurn(d, id)
		statuslines(d, id, 3)
		ncTurnEnds(d, id)
		statuslines(d, id, 3)
	}
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("cleared with a handoff on disk and no ack: %q", f.written())
	}
	if step(d, id) != NewContextLimit {
		t.Fatalf("the cycle gave up or moved on with no ack: %v", d.newContextFor(id))
	}
}

// The ack, then /clear, then the wake, and the handoff kept on the card.
func TestAckThenClearThenWake(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := cycleCard(t, d, 220_000)
	id := task.ID
	path := d.handoffPath(task)
	midTurn(d, id)
	statuslines(d, id, 1)
	until(t, "the limit prompt", func() bool { return prompts(f) == 1 })

	// The agent writes its handoff and runs `atrium ready`, inside its turn.
	text := "## doing\nthe context cycle\n## left\nthe board\n"
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	out, _, err := d.ready(task)
	if err != nil {
		t.Fatal(err)
	}
	if out["message"] != ReadyLine || out["path"] != path || out["stored"] != true {
		t.Fatalf("ready answered %v", out)
	}
	time.Sleep(80 * time.Millisecond)
	if strings.Contains(f.written(), "/clear") {
		t.Fatalf("/clear was typed into the turn that ran atrium ready: %q", f.written())
	}
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.wake.sawSession(id, time.Now())
	until(t, "the wake", func() bool { return strings.Contains(f.written(), "read "+path+" and continue.") })
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
	got := f.written()
	if !(strings.Index(got, limitPrompt) < strings.Index(got, "/clear") &&
		strings.Index(got, "/clear") < strings.Index(got, "read "+path)) {
		t.Fatalf("out of order: %q", got)
	}

	// The copy on the card.
	evs, err := d.st.Events(id, 50)
	if err != nil {
		t.Fatal(err)
	}
	var stored map[string]any
	for _, e := range evs {
		var p map[string]any
		if e.Kind == store.EventNotified && json.Unmarshal(e.Payload, &p) == nil && p["handoff"] != nil {
			stored = p
		}
	}
	if stored == nil || stored["handoff"] != text || stored["path"] != path || stored["by"] != cycleBy {
		t.Fatalf("the handoff is not on the card: %v", stored)
	}
}

// Plan 9 and 10: the details switch keeps a card out, and its own limit moves the line.
func TestTheCardSwitchAndOverride(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := cycleCard(t, d, 250_000)
	id := task.ID

	if err := d.st.SetOverrides(id, map[string]string{api.OverrideContextCycle: "off"}); err != nil {
		t.Fatal(err)
	}
	statuslines(d, id, 3)
	watch(t, d)
	time.Sleep(50 * time.Millisecond)
	if d.newContextFor(id) != nil || prompts(f) != 0 {
		t.Fatalf("a card switched off was cycled: %v %q", d.newContextFor(id), f.written())
	}
	if cs, _ := d.contextSizeFor(id).(*ContextSize); cs == nil || cs.Cycle || !cs.Warn {
		t.Fatalf("a switched-off card reads %+v, want warned and not cycling", cs)
	}

	// Its own limit, above the size: on, and still under.
	if err := d.st.SetOverrides(id, map[string]string{api.OverrideContextCycle: "", api.OverrideContextLimitK: "300"}); err != nil {
		t.Fatal(err)
	}
	statuslines(d, id, 3)
	time.Sleep(50 * time.Millisecond)
	if d.newContextFor(id) != nil {
		t.Fatalf("a card under its own limit was cycled: %v", d.newContextFor(id))
	}
	if cs, _ := d.contextSizeFor(id).(*ContextSize); cs == nil || !cs.Cycle || cs.Warn || cs.Source != "card" {
		t.Fatalf("the card reads %+v, want cycling, under its own 300k", cs)
	}

	// Below the size: cycled.
	if err := d.st.SetOverrides(id, map[string]string{api.OverrideContextLimitK: "240"}); err != nil {
		t.Fatal(err)
	}
	statuslines(d, id, 1)
	until(t, "the limit prompt", func() bool { return prompts(f) == 1 })
	if v, _ := d.newContextFor(id).(map[string]any); v["limit"] != int64(240_000) {
		t.Fatalf("the cycle started on %v, want the card's 240k", v["limit"])
	}
}

// The hub's per-harness limit is the one held to, when the card has none.
func TestTheHubLimitStartsTheCycle(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := cycleCard(t, d, 150_000)
	statuslines(d, task.ID, 2)
	if d.newContextFor(task.ID) != nil {
		t.Fatal("cycled under the default 200k")
	}
	if err := d.st.SetSetting(store.SettingContextLimits, `{"claude":120}`); err != nil {
		t.Fatal(err)
	}
	watch(t, d)
	until(t, "the limit prompt", func() bool { return prompts(f) == 1 })
}

// The runner compacted under its limit before the ack: nothing to cycle, so the
// cycle ends quietly and what it held is let go.
func TestACardBackUnderItsLimitDropsTheCycle(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, reply := cycleCard(t, d, 250_000)
	id := task.ID
	statuslines(d, id, 1)
	until(t, "the limit prompt", func() bool { return prompts(f) == 1 })
	reply(90_000)
	statuslines(d, id, 1)
	until(t, "the chip to go", func() bool { return d.newContextFor(id) == nil })
	if d.holdingMessages(id) || strings.Contains(f.written(), "/clear") {
		t.Fatalf("a dropped cycle still holds or cleared: %q", f.written())
	}
	if !historyHas(t, d, id, "fell back under its limit") {
		t.Fatal("the history does not say why the cycle ended")
	}
}

// `atrium ready` is refused with no cycle waiting, and with no handoff written, and
// the cycle keeps waiting.
func TestReadyIsRefusedWithNoCycleOrNoHandoff(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	task, f, _ := cycleCard(t, d, 100_000)
	id := task.ID

	post := func() *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/ready", strings.NewReader(`{"agent":"x","task_id":"`+id+`"}`))
		rec := httptest.NewRecorder()
		d.handleReady(rec, req)
		return rec
	}
	if rec := post(); rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "no context cycle") {
		t.Fatalf("ready with no cycle answered %d %s", rec.Code, rec.Body)
	}
	if err := d.StartNewContext(id); err != nil {
		t.Fatal(err)
	}
	until(t, "the limit prompt", func() bool { return prompts(f) == 1 })
	rec := post()
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), d.handoffPath(task)) {
		t.Fatalf("ready with no file answered %d %s", rec.Code, rec.Body)
	}
	if err := os.WriteFile(d.handoffPath(task), []byte("  \n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := post(); rec.Code != http.StatusConflict {
		t.Fatalf("ready with an empty file answered %d %s", rec.Code, rec.Body)
	}
	time.Sleep(50 * time.Millisecond)
	if step(d, id) != NewContextLimit || strings.Contains(f.written(), "/clear") {
		t.Fatalf("a refused ack moved the cycle: %v", d.newContextFor(id))
	}
	if err := os.WriteFile(d.handoffPath(task), handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if rec := post(); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), ReadyLine) {
		t.Fatalf("ready answered %d %s", rec.Code, rec.Body)
	}
	if rec := post(); rec.Code != http.StatusConflict {
		t.Fatalf("a second ready answered %d", rec.Code)
	}
}

// A card atrium does not supervise, or a fixture, is never cycled.
func TestOnlySupervisedCardsCycle(t *testing.T) {
	fastNewContext(t)
	d := testDaemon(t)
	bare := cardFor(t, d, "not-ours")
	if d.cycleSubject(bare) {
		t.Fatal("a card with no terminal here is a subject")
	}
	task, _, _ := cycleCard(t, d, 250_000)
	if !d.cycleSubject(task) {
		t.Fatal("a person's own supervised card is not a subject")
	}
	// Old tags are ignored: neither keeps a card out nor puts one in.
	tagged := *task
	tagged.Tags = []string{"atrium:no-auto-new-context", "atrium:subagent"}
	if !d.cycleSubject(&tagged) {
		t.Fatal("an old context tag kept a card out")
	}
}

// The limit prompt names the binary and the path, in clint's words.
func TestTheLimitPromptWording(t *testing.T) {
	got := newContextLimitPrompt("/tmp/atrium/handoffs/c1.md", "/usr/local/bin/atrium")
	want := "you are at context limit. wrap what is in flight, write your handoff to /tmp/atrium/handoffs/c1.md, " +
		"then run /usr/local/bin/atrium ready."
	if got != want {
		t.Fatalf("got %q", got)
	}
}
