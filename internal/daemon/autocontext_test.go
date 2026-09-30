package daemon

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The automatic new context. See autocontext.go and docs/runtime/auto-new-context-design.md,
// whose section 9 is this test list. The r-030 capture guard has its own file.

// fastAuto shrinks the automatic timings to milliseconds and puts them back. The restart
// grace is off, the gap is long and the retry is short, and a test that wants any of them
// otherwise sets that one after.
func fastAuto(t *testing.T) {
	t.Helper()
	fastNewContext(t)
	old := autoTiming
	autoTiming.startGrace = 0
	autoTiming.minGap = time.Hour
	autoTiming.retryAfter = 30 * time.Millisecond
	autoTiming.typeWait = 60 * time.Millisecond
	autoTiming.humanQuiet = 0
	// The 10 second floor of the idle setting is then a millisecond.
	autoTiming.idleUnit = 100 * time.Microsecond
	t.Cleanup(func() { autoTiming = old })
}

var replyN atomic.Int64

// appendReply writes one main reply at a context size to a transcript.
func appendReply(t *testing.T, path string, tokens int64) {
	t.Helper()
	n := replyN.Add(1)
	line := fmt.Sprintf(`{"type":"assistant","timestamp":%q,"message":{"model":"claude-opus-5-5",`+
		`"usage":{"input_tokens":1,"cache_creation_input_tokens":0,"cache_read_input_tokens":%d}}}`+"\n",
		time.Now().UTC().Add(time.Duration(n)*time.Second).Format(time.RFC3339Nano), tokens-1)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line); err != nil {
		t.Fatal(err)
	}
}

// autoRig is one card the automatic cycle may act on: a terminal, a directory for its
// handoff, a transcript, and the tags it wears.
type autoRig struct {
	t        *testing.T
	d        *Daemon
	task     *store.Task
	f        *fakePTY
	run      *runner
	dir      string
	path     string
	launcher *store.Task
}

// newAutoRig makes a Claude card between turns. mode is the setting, "" leaving it unset.
// A card wearing OriginAgentTag is given a launcher, so its notices have somewhere to go.
func newAutoRig(t *testing.T, mode string, tags ...string) *autoRig {
	t.Helper()
	fastAuto(t)
	d := testDaemon(t)
	return rigOn(t, d, "cycler", "claude", mode, tags...)
}

func rigOn(t *testing.T, d *Daemon, name, runner, mode string, tags ...string) *autoRig {
	t.Helper()
	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: name, Worktree: filepath.ToSlash(dir), Runner: runner, PID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	r := &autoRig{t: t, d: d, dir: dir, path: filepath.Join(t.TempDir(), "t.jsonl")}
	r.run, r.f = typedRunner(t, d, task.ID)
	t.Cleanup(func() { d.pending.stopAll(); d.nctx.stopAll() })
	if mode != "" {
		if err := d.st.SetSetting(store.SettingAutoNewContext, mode); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.st.SetTags(task.ID, tags); err != nil {
		t.Fatal(err)
	}
	if hasTag(tags, OriginAgentTag) {
		r.launcher = peerCard(t, d, "boss")
		if err := d.st.SetLineage(task.ID, "boss", r.launcher.ID); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.st.SetResumeID(task.ID, "sess-"+name); err != nil {
		t.Fatal(err)
	}
	d.ctx.transcript = func(cwd, id string) string {
		if id == "sess-"+name {
			return r.path
		}
		return ""
	}
	r.task = r.fresh()
	return r
}

func (r *autoRig) fresh() *store.Task {
	r.t.Helper()
	task, err := r.d.st.Get(r.task0())
	if err != nil {
		r.t.Fatal(err)
	}
	return task
}

func (r *autoRig) task0() string {
	if r.task != nil {
		return r.task.ID
	}
	all, _ := r.d.st.List()
	for _, x := range all {
		if x.Worktree == filepath.ToSlash(r.dir) {
			return x.ID
		}
	}
	return ""
}

func (r *autoRig) id() string { return r.task.ID }

// size writes a reply at a context size, in thousands.
func (r *autoRig) size(k int64) { appendReply(r.t, r.path, k*1000) }

// tick is one pass of the reaper's context watcher.
func (r *autoRig) tick() { watch(r.t, r.d) }

// begun is whether a cycle has been claimed on the card.
func (r *autoRig) begun() bool { return r.d.nctx.get(r.id()) != nil }

// fired is whether a tick has started an automatic run on the card.
func (r *autoRig) fired() bool {
	s := r.d.auto.get(r.id())
	return s != nil && s.state == autoFired
}

// wantNone runs a tick and fails when it started a cycle.
func (r *autoRig) wantNone(why string) {
	r.t.Helper()
	r.tick()
	if r.fired() || r.f.written() != "" {
		r.t.Fatalf("a cycle started %s (typed %q)", why, r.f.written())
	}
}

// wantStart runs a tick and fails when it did not start a cycle.
func (r *autoRig) wantStart(why string) {
	r.t.Helper()
	r.tick()
	if !r.fired() {
		r.t.Fatalf("no cycle started %s", why)
	}
}

// autoQuiet lets the idle quiet of an agent card pass.
func autoQuiet() { time.Sleep(4 * time.Millisecond) }

// capturePrompted waits for the capture prompt to be typed.
func (r *autoRig) capturePrompted() {
	r.t.Helper()
	until(r.t, "the capture prompt", func() bool {
		return strings.Contains(r.f.written(), "HANDOFF.") && strings.HasSuffix(r.f.written(), "\r")
	})
}

// captureTurn is the card doing what the capture asks: a turn, a handoff, the turn over.
func (r *autoRig) captureTurn() {
	r.t.Helper()
	r.capturePrompted()
	r.d.act.set(r.id(), ActivityThinking, "")
	if err := os.WriteFile(filepath.Join(r.dir, HandoffName(r.task)), handoffBody, 0o644); err != nil {
		r.t.Fatal(err)
	}
	time.Sleep(5 * time.Millisecond)
	ncTurnEnds(r.d, r.id())
}

// newSession is the runner starting the conversation after a /clear.
func (r *autoRig) newSession(conv string) {
	r.t.Helper()
	until(r.t, "/clear", func() bool { return strings.Contains(r.f.written(), "/clear") })
	r.d.wake.sawConversation(r.id(), conv)
	r.d.wake.sawSession(r.id(), time.Now())
}

// cycle drives one whole run to its end and leaves the card reading `after` thousand.
func (r *autoRig) cycle(conv string, after int64) {
	r.t.Helper()
	r.captureTurn()
	r.newSession(conv)
	until(r.t, "the wake prompt", func() bool {
		return strings.Contains(r.f.written(), newContextWake(HandoffName(r.task)))
	})
	until(r.t, "the chip to go", func() bool { return r.d.newContextFor(r.id()) == nil })
	r.size(after)
}

// notified is the payloads of the card's `notified` events written by `by`.
func (r *autoRig) notified(by string) []map[string]any {
	r.t.Helper()
	evs, err := r.d.st.Events(r.id(), 500)
	if err != nil {
		r.t.Fatal(err)
	}
	var out []map[string]any
	for _, e := range evs {
		if e.Kind != store.EventNotified {
			continue
		}
		var p map[string]any
		if json.Unmarshal(e.Payload, &p) == nil && p["by"] == by {
			out = append(out, p)
		}
	}
	return out
}

// autoNotices is the launcher's queued auto new context notices.
func (r *autoRig) autoNotices() []string {
	r.t.Helper()
	var out []string
	if r.launcher == nil {
		return nil
	}
	for _, m := range pendingFrom(r.t, r.d, r.launcher.ID) {
		if strings.Contains(m.Text, "cycling its context") || strings.Contains(m.Text, "could not cycle") ||
			strings.Contains(m.Text, "auto new context ran") {
			out = append(out, m.Text)
		}
	}
	return out
}

// viewer attaches a browser to the card's terminal, and returns the way to detach it.
func (r *autoRig) viewer() (detach func()) {
	ch := make(chan []byte, 1)
	r.run.mu.Lock()
	r.run.watchers[ch] = struct{}{}
	r.run.mu.Unlock()
	return func() {
		r.run.mu.Lock()
		delete(r.run.watchers, ch)
		r.run.mu.Unlock()
	}
}

const bigK = 320

// With the setting unset a huge card is left alone, and so with it off outright.
func TestAutoContextOffByDefault(t *testing.T) {
	r := newAutoRig(t, "", AutoContextTag)
	r.size(900)
	r.wantNone("with the setting unset")
	if got := r.d.st.AutoNewContextMode(); got != store.AutoNewContextOff {
		t.Fatalf("the default is %q, not off", got)
	}
	if err := r.d.st.SetSetting(store.SettingAutoNewContext, "off"); err != nil {
		t.Fatal(err)
	}
	r.wantNone("with the setting off")
	if err := r.d.st.SetSetting(store.SettingAutoNewContext, "garbage"); err != nil {
		t.Fatal(err)
	}
	r.wantNone("with the setting unreadable")
}

// Which cards each setting reaches. Every row is a card past the line and quiet.
func TestAutoContextSubjectMatrix(t *testing.T) {
	human := []string{}
	tagged := []string{AutoContextTag}
	worker := []string{OriginAgentTag, SubagentTag}
	taggedWorker := []string{OriginAgentTag, SubagentTag, AutoContextTag}
	director := []string{OriginAgentTag, DirectorTag}
	noAuto := []string{OriginAgentTag, DirectorTag, AutoContextTag, NoAutoContextTag}
	noAutoHuman := []string{AutoContextTag, NoAutoContextTag}

	rows := []struct {
		name string
		tags []string
		// off, tagged, agents
		want [3]bool
		// odd is a way to make the card one the setting never reaches.
		odd string
	}{
		{"a human card", human, [3]bool{false, false, false}, ""},
		{"a tagged human card", tagged, [3]bool{false, true, true}, ""},
		{"an agent worker", worker, [3]bool{false, false, false}, ""},
		{"a tagged agent worker", taggedWorker, [3]bool{false, true, true}, ""},
		{"a director", director, [3]bool{false, false, true}, ""},
		{"the no-auto tag on a director", noAuto, [3]bool{false, false, false}, ""},
		{"the no-auto tag on a tagged human card", noAutoHuman, [3]bool{false, false, false}, ""},
		{"a fixture", tagged, [3]bool{false, false, false}, "fixture"},
		{"a throwaway", tagged, [3]bool{false, false, false}, "throwaway"},
		{"a card with a lent session", tagged, [3]bool{false, false, false}, "guest"},
		{"a non-Claude runner", tagged, [3]bool{false, false, false}, "other"},
	}
	for _, row := range rows {
		for i, mode := range []string{store.AutoNewContextOff, store.AutoNewContextTagged, store.AutoNewContextAgents} {
			t.Run(row.name+"/"+mode, func(t *testing.T) {
				fastAuto(t)
				d := testDaemon(t)
				runnerName := "claude"
				if row.odd == "other" {
					if _, err := d.st.SaveHarness(store.Harness{ID: "plainsh", Cmd: "sh", LaunchMode: store.LaunchPTY}); err != nil {
						t.Fatal(err)
					}
					runnerName = "plainsh"
				}
				r := rigOn(t, d, "card", runnerName, mode, row.tags...)
				switch row.odd {
				case "fixture":
					if _, err := d.st.SaveFixture(&store.Fixture{Harness: "claude", TaskID: r.id(), Enabled: true}); err != nil {
						t.Fatal(err)
					}
				case "throwaway":
					if err := d.st.SetThrowaway(r.id(), true); err != nil {
						t.Fatal(err)
					}
				case "guest":
					d.guests.put(&guestShare{TaskID: r.id()})
				}
				r.size(bigK)
				autoQuiet()
				r.tick()
				if got := r.fired(); got != row.want[i] {
					t.Fatalf("%s under %s: started=%v, want %v", row.name, mode, got, row.want[i])
				}
			})
		}
	}
}

// After a /clear the resume id still names the old huge transcript until the next Stop
// (item 62). The card is read from the session its runner last started.
func TestAutoContextThresholdFromSessionNotResumeID(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	old := filepath.Join(t.TempDir(), "old.jsonl")
	fresh := filepath.Join(t.TempDir(), "new.jsonl")
	appendReply(t, old, 900_000)
	appendReply(t, fresh, 20_000)
	r.d.ctx.transcript = func(cwd, id string) string {
		switch id {
		case "sess-cycler":
			return old
		case "sess-new":
			return fresh
		}
		return ""
	}
	r.d.ctx.started(r.id(), "sess-new")
	autoQuiet()
	r.wantNone("on the old transcript after a /clear")
	// Control: with the runner not having said, the old one is read and it does fire.
	r.d.ctx.mu.Lock()
	delete(r.d.ctx.session, r.id())
	r.d.ctx.mu.Unlock()
	r.wantStart("on the resume id's transcript before any SessionStart")
}

// The line is the setting, or 70 percent of the window the statusline reported when that is
// lower. A stale window and no window are the setting alone.
func TestAutoContextThresholdUsesWindow(t *testing.T) {
	t.Run("a fresh 200k window fires at 140k", func(t *testing.T) {
		r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
		r.d.act.setTelemetry(r.id(), Telemetry{Window: 200_000})
		r.size(139)
		autoQuiet()
		r.wantNone("under 70 percent of the window")
		r.size(141)
		r.wantStart("over 70 percent of the window")
		if c := r.d.nctx.get(r.id()); c == nil || c.threshold != 140_000 {
			t.Fatalf("the run recorded the line %+v, want 140000", c)
		}
	})
	t.Run("a 1M window leaves the setting", func(t *testing.T) {
		r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
		r.d.act.setTelemetry(r.id(), Telemetry{Window: 1_000_000})
		r.size(299)
		autoQuiet()
		r.wantNone("under the setting")
		r.size(301)
		r.wantStart("over the setting")
	})
	t.Run("a stale window falls back to the setting", func(t *testing.T) {
		r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
		r.d.act.setTelemetry(r.id(), Telemetry{Window: 200_000})
		later := time.Now().Add(telemetryStaleAfter + time.Minute)
		r.d.act.now = func() time.Time { return later }
		r.size(200)
		r.wantNone("on a window nobody has heard from for an hour")
	})
	t.Run("no telemetry falls back to the setting", func(t *testing.T) {
		r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
		r.size(200)
		autoQuiet()
		r.wantNone("with no window")
		r.size(310)
		r.wantStart("over the setting")
	})
}

// A card whose window is small is cycled before the sa87 notice, and its launcher is told
// when the run begins, before anything is typed.
func TestAutoContextBelowNoticeTellsLauncherFirst(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.d.act.setTelemetry(r.id(), Telemetry{Window: 200_000})
	r.size(141)
	autoQuiet()
	r.wantStart("at 70 percent of a 200k window")
	if n := len(contextNotices(t, r.d, r.launcher.ID)); n != 0 {
		t.Fatalf("the sa87 notice was sent %d times before the run", n)
	}
	// The launcher has the notice by the moment the capture prompt is typed.
	r.capturePrompted()
	got := r.autoNotices()
	if len(got) != 1 || !strings.Contains(got[0], "reached 141k") || !strings.Contains(got[0], HandoffName(r.task)) {
		t.Fatalf("the launcher was not told before the capture prompt: %q", got)
	}
}

// A tagged human card needs nobody at its terminal and a long quiet, not the 45 seconds.
func TestAutoContextHumanCardNeedsNoViewerAndLongQuiet(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	r.size(bigK)
	autoQuiet()

	detach := r.viewer()
	r.wantNone("with a browser attached")
	detach()

	// The agent quiet has passed, the human's has not.
	autoTiming.humanQuiet = time.Hour
	r.wantNone("inside the human card's ten minutes")

	autoTiming.humanQuiet = 0
	r.wantStart("with nobody attached and the quiet over")
}

// A viewer that attaches after the tick and before the capture prompt stops the run with
// nothing typed, and no chip, and no event.
func TestAutoContextViewerBeforeCapturePromptStopsRun(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	r.size(bigK)
	autoQuiet()
	// Held on a part written line so the run waits in autoPrepare while a viewer arrives.
	r.closeLine()
	r.wantStart("with nobody attached")
	r.viewer()
	until(t, "the run to stand aside", func() bool { return !r.begun() })
	if r.f.written() != "" {
		t.Fatalf("typed into a card a person is watching: %q", r.f.written())
	}
	if n := len(r.notified(autoContextBy)); n != 0 {
		t.Fatalf("a dropped attempt left %d events", n)
	}
}

// Each of these holds the start, and it goes on the tick after the condition clears.
func TestAutoContextWaitsOutTheStateGates(t *testing.T) {
	gates := []struct {
		name  string
		on    func(r *autoRig)
		clear func(r *autoRig)
	}{
		{"a turn running", func(r *autoRig) { r.d.act.set(r.id(), ActivityThinking, "") },
			func(r *autoRig) { ncTurnEnds(r.d, r.id()) }},
		{"the card running", func(r *autoRig) { _ = r.d.st.SetStatus(r.id(), store.StatusRunning) },
			func(r *autoRig) { _ = r.d.st.SetStatus(r.id(), store.StatusNeedsInput) }},
		{"a permission needed", func(r *autoRig) { _ = r.d.st.SetStatus(r.id(), store.StatusNeedsPermission) },
			func(r *autoRig) { _ = r.d.st.SetStatus(r.id(), store.StatusNeedsInput) }},
		{"a pending permission", func(r *autoRig) {
			if _, _, err := r.d.st.RecordPermission(r.id(), "Bash", "ls", "k1", ""); err != nil {
				r.t.Fatal(err)
			}
		}, func(r *autoRig) {
			ps, _ := r.d.st.PendingForTask(r.id())
			for _, p := range ps {
				if _, err := r.d.st.DecidePermission(p.ID, "approve", ""); err != nil {
					r.t.Fatal(err)
				}
			}
		}},
		{"a dialog open", func(r *autoRig) { r.d.act.dialogRaised(r.id()) },
			func(r *autoRig) { r.d.act.set(r.id(), ActivityIdle, "") }},
		{"subagents running", func(r *autoRig) { r.d.act.setBackground(r.id(), 1) },
			func(r *autoRig) { r.d.act.setBackground(r.id(), 0) }},
		{"a message queued", func(r *autoRig) {
			if _, err := r.d.st.QueueMessage(r.id(), "look at this first"); err != nil {
				r.t.Fatal(err)
			}
		}, func(r *autoRig) {
			var ids []string
			for _, m := range pendingFrom(r.t, r.d, r.id()) {
				ids = append(ids, m.ID)
			}
			if err := r.d.st.MarkDelivered(r.id(), "test", ids); err != nil {
				r.t.Fatal(err)
			}
		}},
		{"a chip held", func(r *autoRig) { r.d.nctx.claim(r.id(), &newContext{step: NewContextCapture}) },
			func(r *autoRig) { r.d.nctx.clear(r.id()) }},
		{"an idle handoff capturing", func(r *autoRig) {
			r.d.idle.put(r.id(), &handoffMark{capturing: true})
		}, func(r *autoRig) { r.d.idle.drop(r.id()) }},
		{"a card in the same directory mid cycle", func(r *autoRig) {
			other, _, err := r.d.st.Register(store.Observed{
				WireName: "neighbour", Worktree: filepath.ToSlash(r.dir), Runner: "claude", PID: 2,
			})
			if err != nil {
				r.t.Fatal(err)
			}
			r.d.nctx.claim(other.ID, &newContext{step: NewContextCapture})
			r.t.Cleanup(func() { r.d.nctx.clear(other.ID) })
		}, nil},
	}
	for _, g := range gates {
		t.Run(g.name, func(t *testing.T) {
			r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
			r.size(bigK)
			autoQuiet()
			g.on(r)
			r.wantNone("with " + g.name)
			r.wantNone("with " + g.name + " on the next tick")
			if g.clear == nil {
				return
			}
			g.clear(r)
			autoQuiet()
			r.wantStart("once " + g.name + " cleared")
		})
	}
}

// Statuses the tick does not list, and a parked card. None start.
func TestAutoContextNeverParkedShelvedDone(t *testing.T) {
	for _, st := range []string{store.StatusDone, store.StatusDead, store.StatusShelved} {
		t.Run(st, func(t *testing.T) {
			r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
			r.size(bigK)
			autoQuiet()
			if err := r.d.st.SetStatus(r.id(), st); err != nil {
				t.Skipf("status %s cannot be set: %v", st, err)
			}
			r.wantNone("on a card that is " + st)
		})
	}
	t.Run("parked", func(t *testing.T) {
		r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
		r.size(bigK)
		autoQuiet()
		parkFixture(t, r)
		r.wantNone("on a parked card")
		// Called directly as well, past the list: the gate on the card itself.
		if r.d.autoReady(r.fresh(), false, time.Now()) {
			t.Fatal("autoReady passed a parked card")
		}
	})
}

// Unsent characters on the line are not typed into. The attempt is dropped after typeWait
// with no chip and no event, and goes again on a later tick.
func TestAutoContextTypedLineGate(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	r.size(bigK)
	autoQuiet()
	r.closeLine()
	r.wantStart("with a part written line, the gate is at the line and not the tick")
	until(t, "the attempt to be dropped", func() bool { return !r.begun() })
	if r.f.written() != "" {
		t.Fatalf("typed over the line: %q", r.f.written())
	}
	if r.d.newContextFor(r.id()) != nil {
		t.Fatal("a dropped attempt left a chip")
	}
	if n := len(r.notified(autoContextBy)); n != 0 {
		t.Fatalf("a dropped attempt wrote %d events", n)
	}
	if evs := r.notified(newContextBy); len(evs) != 0 {
		t.Fatalf("a dropped attempt wrote manual events: %v", evs)
	}
	if s := r.d.auto.get(r.id()); s == nil || s.state != autoArmed || !s.firedAt.IsZero() {
		t.Fatalf("a dropped attempt left the arm state %+v, want armed with no gap", s)
	}

	// The line clears, and a later tick starts it again, past the gap a run would have set.
	r.openLine()
	r.wantStart("on a later tick, with the line empty")
	r.capturePrompted()
}

// Two ticks and a press racing make one claim and one set of prompts.
func TestAutoContextNeverTwice(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	r.size(bigK)
	autoQuiet()
	r.tick()
	r.tick()
	if err := r.d.StartNewContext(r.id()); err != errNewContextBusy {
		t.Fatalf("a press over the automatic run: %v", err)
	}
	r.capturePrompted()
	time.Sleep(30 * time.Millisecond)
	if n := strings.Count(r.f.written(), "Your context is about to be cleared"); n != 1 {
		t.Fatalf("the capture prompt was typed %d times: %q", n, r.f.written())
	}
	if n := len(r.notified(autoContextBy)); n != 1 {
		t.Fatalf("%d start events", n)
	}
}

// Nothing fires in the first startGrace after the daemon starts.
func TestAutoContextRestartGrace(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	autoTiming.startGrace = time.Hour
	r.size(bigK)
	autoQuiet()
	r.wantNone("inside the restart grace")
	autoTiming.startGrace = 0
	r.wantStart("after it")
}

// The default is off, a run is not left holding when the setting is turned off mid way, and
// the tag can name a card the setting would not reach.
func TestAutoContextSettingOffMidRunDropsBeforeTyping(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag)
	r.size(bigK)
	autoQuiet()
	r.closeLine()
	r.wantStart("first")
	if err := r.d.st.SetSetting(store.SettingAutoNewContext, "off"); err != nil {
		t.Fatal(err)
	}
	until(t, "the run to stand aside", func() bool { return !r.begun() })
	if r.f.written() != "" {
		t.Fatalf("typed after the setting went off: %q", r.f.written())
	}
}

// The started event and the launcher's notice go out just before the capture prompt is
// typed, so an attempt that is dropped before then records nothing (decided in r-029b).
func TestAutoContextStartRecordedBeforeCapturePrompt(t *testing.T) {
	r := newAutoRig(t, store.AutoNewContextTagged, AutoContextTag, OriginAgentTag)
	r.size(bigK)
	autoQuiet()
	r.wantStart("first")
	r.capturePrompted()
	evs := r.notified(autoContextBy)
	if len(evs) != 1 || evs[0]["started"] != true || evs[0]["tokens"] != float64(bigK*1000) ||
		evs[0]["file"] != HandoffName(r.task) || evs[0]["attempt"] != float64(1) {
		t.Fatalf("start events %v", evs)
	}
	if n := len(r.autoNotices()); n != 1 {
		t.Fatalf("%d notices by the time the capture prompt was typed", n)
	}
}

// parkFixture parks the card the way the daemon does, without a process to end.
func parkFixture(t *testing.T, r *autoRig) {
	t.Helper()
	if err := r.d.parkCard(r.id(), store.StatusNeedsInput, nil); err != nil {
		t.Fatal(err)
	}
}

// closeLine leaves unsent characters on the card's line.
func (r *autoRig) closeLine() { r.run.noteOperatorTyped([]byte("half a thou")) }

// openLine sends the line and lets the keyboard go quiet, so the gate is open.
func (r *autoRig) openLine() {
	r.run.noteOperatorTyped([]byte("\r"))
	r.run.typeMu.Lock()
	r.run.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.run.typeMu.Unlock()
}
