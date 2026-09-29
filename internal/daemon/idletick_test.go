package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-007 stage 5: the idle clock, the director's handoff, and the park.

// quickPark makes the wind-down instant: the runner is asked to leave by
// dropping it, as its exit would.
func quickPark(t *testing.T) {
	t.Helper()
	oldLeave, oldWait := idleLeave, idleGoneWait
	idleLeave = func(d *Daemon, r *runner, id string) { endRunner(d, id) }
	idleGoneWait = 200 * time.Millisecond
	t.Cleanup(func() { idleLeave, idleGoneWait = oldLeave, oldWait })
}

// idleAgent is an agent card with a runner, sitting at its prompt.
func idleAgent(t *testing.T, d *Daemon, name, status string, tags ...string) *store.Task {
	t.Helper()
	card := peerCard(t, d, name)
	if err := d.st.SetTags(card.ID, append([]string{OriginAgentTag}, tags...)); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(card.ID, status); err != nil {
		t.Fatal(err)
	}
	liveRunner(d, card.ID)
	got, err := d.st.Get(card.ID)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func parkedNow(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return isParked(got)
}

// settles waits for a park in flight, or for it to have not happened.
func settles(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if parkedNow(t, d, id) {
			return true
		}
		time.Sleep(10 * time.Millisecond)
	}
	return false
}

// settlesTo is settles for a card that should NOT park: it looks after a short
// wait, since waiting seconds to be sure of a negative adds up across a table.
func settlesTo(t *testing.T, d *Daemon, id string, want bool) bool {
	t.Helper()
	if want {
		return settles(t, d, id)
	}
	time.Sleep(250 * time.Millisecond)
	return parkedNow(t, d, id)
}

func idleAfter(d time.Duration) time.Time { return time.Now().Add(d) }

func TestIdleParkRule(t *testing.T) {
	quickPark(t)
	cases := []struct {
		name   string
		status string
		mut    func(d *Daemon, c *store.Task)
		parks  bool
	}{
		{"the all-clear", store.StatusNeedsInput, nil, true},
		{"done at its prompt", store.StatusDone, nil, true},
		{"running", store.StatusRunning, nil, false},
		{"needs permission", store.StatusNeedsPermission, nil, false},
		{"a pending permission", store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			if _, _, err := d.st.RecordPermission(c.ID, "Bash", "ls", "k1", ""); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a queued message", store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			if _, err := d.st.QueueMessage(c.ID, "hello"); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a pending restart wake", store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			if _, _, err := d.st.SetRestartWake(c.ID, "wake up", "test"); err != nil {
				t.Fatal(err)
			}
		}, false},
		{"a live worker", store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			w := peerCard(t, d, "w-"+c.WireName)
			if err := d.st.SetLineage(w.ID, c.WireName, c.ID); err != nil {
				t.Fatal(err)
			}
			w, _ = d.st.Get(w.ID)
			if _, err := d.st.CreateWorkItem(w, store.NewWorkItem{Brief: "b"}); err != nil {
				t.Fatal(err)
			}
			liveRunner(d, w.ID)
		}, false},
		{"background work as the last Stop reported", store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			d.act.setBackgroundWork(c.ID, 1)
		}, false},
		{"subagents as the last Stop reported", store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			d.act.setBackground(c.ID, 2)
		}, false},
		{"mid new-context",store.StatusNeedsInput, func(d *Daemon, c *store.Task) {
			d.nctx.begin(c.ID, "HANDOFF.x.md", "")
		}, false},
	}
	for i, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			d := testDaemon(t)
			card := idleAgent(t, d, "rule"+string(rune('a'+i)), c.status)
			if c.mut != nil {
				c.mut(d, card)
			}
			d.parkIdle(idleAfter(3 * time.Hour))
			got := settles(t, d, card.ID)
			if got != c.parks {
				t.Fatalf("parked=%v, want %v", got, c.parks)
			}
		})
	}
}

func TestIdleClockIgnoresHandoffTurn(t *testing.T) {
	d := testDaemon(t)
	card := idleAgent(t, d, "clock", store.StatusNeedsInput, DirectorTag)
	before := d.idleSince(card)
	d.idle.put(card.ID, &handoffMark{base: before, capturing: true})
	// The capture prompt is recorded as a prompt, which stamps prompted_at.
	if err := d.st.AppendEvent(card.ID, store.EventPrompted, map[string]any{"text": "capture", "via": "terminal"}); err != nil {
		t.Fatal(err)
	}
	now, _ := d.st.Get(card.ID)
	if got := d.idleSince(now); !got.Equal(before) {
		t.Fatalf("the capture moved the idle clock: %v -> %v", before, got)
	}
	// Ended, still inside the slack: still the handoff's own.
	d.idle.put(card.ID, &handoffMark{base: before, end: time.Now(), written: true})
	if got := d.idleSince(now); !got.Equal(before) {
		t.Fatalf("the finished capture moved the idle clock: %v -> %v", before, got)
	}
	// Real activity after it restarts the clock and the cycle.
	time.Sleep(5 * time.Millisecond)
	d.idle.put(card.ID, &handoffMark{base: before, end: time.Now().Add(-time.Minute), written: true})
	if err := d.st.AppendEvent(card.ID, store.EventPrompted, map[string]any{"text": "real", "via": "terminal"}); err != nil {
		t.Fatal(err)
	}
	now, _ = d.st.Get(card.ID)
	if got := d.idleSince(now); !got.After(before) {
		t.Fatalf("a real prompt did not restart the clock: %v", got)
	}
	if d.idle.get(card.ID) != nil {
		t.Fatal("the handoff mark survived real activity")
	}
}

func TestIdleClockReadsHumanTouch(t *testing.T) {
	quickPark(t)
	d := testDaemon(t)
	card := idleAgent(t, d, "touched", store.StatusNeedsInput)
	if err := d.st.TouchHuman(card.ID, ViaTyped, time.Now().Add(2*time.Hour+50*time.Minute)); err != nil {
		t.Fatal(err)
	}
	d.parkIdle(idleAfter(3 * time.Hour))
	if settles(t, d, card.ID) {
		t.Fatal("parked ten minutes after a person typed")
	}
}

func TestIdleParkWorkersFirst(t *testing.T) {
	quickPark(t)
	d := testDaemon(t)
	_, director, worker := directorRig(t, d)
	// No handoff for this one: the runner here has no terminal to type it into.
	if err := d.st.SetTags(director.ID, []string{OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{director.ID, worker.ID} {
		if err := d.st.SetStatus(id, store.StatusNeedsInput); err != nil {
			t.Fatal(err)
		}
	}
	if err := d.st.SetStatus(worker.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	liveRunner(d, director.ID)
	liveRunner(d, worker.ID)

	d.parkIdle(idleAfter(3 * time.Hour))
	if !settles(t, d, worker.ID) {
		t.Fatal("the done worker did not park")
	}
	if parkedNow(t, d, director.ID) {
		t.Fatal("the director parked while its worker was live")
	}
	// The worker is parked, so the director is now free of it.
	dir, _ := d.st.Get(director.ID)
	if !d.idleParkEligible(dir, map[string]bool{}) {
		t.Fatal("a parked worker still holds its launcher up")
	}
}

func TestIdleParkExemptsOperatorCards(t *testing.T) {
	quickPark(t)
	d := testDaemon(t)
	plain := peerCard(t, d, "operators")
	if err := d.st.SetStatus(plain.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	liveRunner(d, plain.ID)
	opted := peerCard(t, d, "optedin")
	if err := d.st.SetTags(opted.ID, []string{ParkIdleTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(opted.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	// Opted in cards take a handoff first, so give this one nothing to write to.
	d.idle.put(opted.ID, &handoffMark{written: true, end: time.Now().Add(time.Hour), base: opted.CreatedAt, file: "HANDOFF.x.md"})
	liveRunner(d, opted.ID)

	d.parkIdle(idleAfter(3 * time.Hour))
	if !settles(t, d, opted.ID) {
		t.Fatal("an opted-in card was not parked")
	}
	if parkedNow(t, d, plain.ID) {
		t.Fatal("an operator card with no tag was parked")
	}
}

func TestIdleParkSetting(t *testing.T) {
	d := testDaemon(t)
	if got, on := d.st.IdleParkAfter(); !on || got != 2*time.Hour {
		t.Fatalf("default is %v %v, want 2h on", got, on)
	}
	if err := d.st.SetSetting(store.SettingIdleParkAfter, "60"); err != nil {
		t.Fatal(err)
	}
	if got, on := d.st.IdleParkAfter(); !on || got != 30*time.Minute {
		t.Fatalf("under the floor is %v, want it clamped to 30m", got)
	}
	if _, err := store.CheckIdleParkAfter("60"); err == nil {
		t.Fatal("the settings page accepted a value under the floor")
	}
	if err := d.st.SetSetting(store.SettingIdleParkAfter, "off"); err != nil {
		t.Fatal(err)
	}
	quickPark(t)
	card := idleAgent(t, d, "offcard", store.StatusNeedsInput)
	d.parkIdle(idleAfter(100 * time.Hour))
	if settles(t, d, card.ID) {
		t.Fatal("off parked a card")
	}
	if err := d.st.SetSetting(store.SettingIdleParkAfter, "3600"); err != nil {
		t.Fatal(err)
	}
	d.parkIdle(idleAfter(40 * time.Minute))
	if parkedNow(t, d, card.ID) {
		t.Fatal("parked before the setting")
	}
	d.parkIdle(idleAfter(70 * time.Minute))
	if !settles(t, d, card.ID) {
		t.Fatal("not parked after the setting")
	}
}

func TestIdleParkKeepsStatus(t *testing.T) {
	quickPark(t)
	d := testDaemon(t)
	card := idleAgent(t, d, "donecard", store.StatusDone)
	// The wind-down would file it dead. It must come back as done.
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = d.st.SetStatus(card.ID, store.StatusDead)
	}()
	d.parkIdle(idleAfter(3 * time.Hour))
	if !settles(t, d, card.ID) {
		t.Fatal("not parked")
	}
	time.Sleep(300 * time.Millisecond)
	got, _ := d.st.Get(card.ID)
	if got.Status == store.StatusDead && !isParked(got) {
		t.Fatal("filed dead")
	}
}

// A director is asked for its handoff at 50 minutes idle, once, and parked at two
// hours with no second turn.
func TestIdleHandoffBeforeCold(t *testing.T) {
	fastNewContext(t)
	quickPark(t)
	d := testDaemon(t)
	task, f, dir := ncCard(t, d)
	if err := d.st.SetTags(task.ID, []string{OriginAgentTag, DirectorTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}

	// Under 50 minutes: nothing.
	d.parkIdle(idleAfter(40 * time.Minute))
	time.Sleep(50 * time.Millisecond)
	if strings.Contains(f.written(), "HANDOFF.") {
		t.Fatal("a handoff was asked for before 50 minutes")
	}

	// At 55 minutes: the capture prompt.
	d.parkIdle(idleAfter(55 * time.Minute))
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), HandoffName(task)) })
	if parkedNow(t, d, task.ID) {
		t.Fatal("parked at the handoff mark")
	}
	d.act.set(task.ID, ActivityThinking, "")
	if err := os.WriteFile(filepath.Join(dir, HandoffName(task)), []byte("state"), 0o644); err != nil {
		t.Fatal(err)
	}
	d.act.set(task.ID, ActivityIdle, "")
	until(t, "the capture to finish", func() bool {
		m := d.idle.get(task.ID)
		return m != nil && m.written
	})
	// The capture turn did not move the idle clock.
	cur, _ := d.st.Get(task.ID)
	if since := d.idleSince(cur); since.After(task.CreatedAt.Add(time.Second)) {
		t.Fatalf("the handoff moved the idle clock to %v", since)
	}
	// Still in the window: another tick asks for nothing more.
	written := f.written()
	d.parkIdle(idleAfter(60 * time.Minute))
	time.Sleep(50 * time.Millisecond)
	if f.written() != written {
		t.Fatal("a second capture was asked for")
	}
	if parkedNow(t, d, task.ID) {
		t.Fatal("parked before two hours")
	}

	// Two hours: parked, and no second turn.
	d.parkIdle(idleAfter(3 * time.Hour))
	if !settles(t, d, task.ID) {
		t.Fatal("not parked at two hours")
	}
	if strings.Count(f.written(), HandoffName(task)+" in the current") != 1 {
		t.Fatalf("more than one capture prompt: %q", f.written())
	}
	if v, _ := d.st.Setting(store.SettingParkHandoffPrefix + task.ID); !strings.HasPrefix(v, HandoffName(task)) {
		t.Fatalf("the handoff was not recorded for the wake: %q", v)
	}
}

func TestIdleHandoffTimeoutStillParks(t *testing.T) {
	fastNewContext(t)
	ncTiming.captureBegin = 100 * time.Millisecond
	quickPark(t)
	d := testDaemon(t)
	task, f, _ := ncCard(t, d)
	if err := d.st.SetTags(task.ID, []string{OriginAgentTag, DirectorTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	d.parkIdle(idleAfter(55 * time.Minute))
	until(t, "the capture prompt", func() bool { return strings.Contains(f.written(), "HANDOFF.") })
	// No turn ever starts, so the capture gives up.
	until(t, "the capture to give up", func() bool {
		m := d.idle.get(task.ID)
		return m != nil && m.timedOut
	})
	d.parkIdle(idleAfter(3 * time.Hour))
	if !settles(t, d, task.ID) {
		t.Fatal("a failed capture kept the card up")
	}
	got, _ := d.st.Get(task.ID)
	until(t, "the card to say so", func() bool {
		got, _ = d.st.Get(task.ID)
		return strings.Contains(got.Why, "without a handoff")
	})
	// No handoff was written, so the wake has nothing to read.
	if v, _ := d.st.Setting(store.SettingParkHandoffPrefix + task.ID); v != "" {
		t.Fatalf("a wake was recorded for a missing handoff: %q", v)
	}
}

// A card that took a handoff has its wake prompt queued AHEAD of the message
// that woke it, and neither is typed.
func TestUnparkQueuesHandoffWake(t *testing.T) {
	d := testDaemon(t)
	card := parkedCard(t, d, "director-asleep", store.StatusNeedsInput)
	_, f := typedRunner(t, d, card.ID)
	peerCard(t, d, "alice")
	if err := d.st.SetSetting(store.SettingParkHandoffPrefix+card.ID, "HANDOFF.director-asleep.md|7200"); err != nil {
		t.Fatal(err)
	}
	sayWake(t, d, "alice", card.ID, "WAKETEXT")
	msgs := pendingFrom(t, d, card.ID)
	if len(msgs) != 2 {
		t.Fatalf("%d messages queued, want the wake prompt then the message", len(msgs))
	}
	if !strings.Contains(msgs[0].Text, "HANDOFF.director-asleep.md") || !strings.Contains(msgs[1].Text, "WAKETEXT") {
		t.Fatalf("wrong order or text: %q then %q", msgs[0].Text, msgs[1].Text)
	}
	if w := f.written(); strings.Contains(w, "WAKETEXT") || strings.Contains(w, "HANDOFF.") {
		t.Fatalf("something was typed into the woken card: %q", w)
	}
	if v, _ := d.st.Setting(store.SettingParkHandoffPrefix + card.ID); v != "" {
		t.Fatal("the record of the handoff outlived the wake")
	}
}
