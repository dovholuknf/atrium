//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func piRow(t *testing.T, d *Daemon) {
	t.Helper()
	if _, err := d.st.SaveHarness(store.Harness{ID: "t-pi", Label: "pi", Enabled: true, Cmd: "pi",
		LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}
}

func hooklessCard(t *testing.T, d *Daemon, row string) (*store.Task, *runner, *fakePTY) {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: "hookless-" + row, Worktree: "/tmp/atrium-hookless-" + row,
		Runner: row})
	if err != nil {
		t.Fatal(err)
	}
	p := newFakePTY()
	r := &runner{taskID: task.ID, pty: p, started: time.Now().Add(-time.Hour), buf: newRing(1<<16, 80),
		watchers: map[chan []byte]struct{}{}, done: make(chan struct{})}
	d.sup.add(r)
	return task, r, p
}

func statusOf(t *testing.T, d *Daemon, id string) string {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.Status
}

func TestAHooklessCardWaitsWhenItsScreenGoesQuietAndWorksWhenItDraws(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	piRow(t, d)
	task, r, _ := hooklessCard(t, d, "t-pi")
	seen := map[string]*hooklessSeen{}
	now := time.Now()

	r.lastOut.Store(now.Add(-time.Second).UnixNano())
	d.hooklessPass(seen, now)
	if s := statusOf(t, d, task.ID); s != store.StatusRunning {
		t.Fatalf("a drawing screen is %s, wanted running", s)
	}
	now = now.Add(hooklessQuiet)
	d.hooklessPass(seen, now)
	if s := statusOf(t, d, task.ID); s != store.StatusNeedsInput {
		t.Fatalf("a quiet screen is %s, wanted needs-input", s)
	}
	for i := 0; i < hooklessBusyTicks; i++ {
		now = now.Add(time.Second)
		r.lastOut.Store(now.UnixNano())
		d.hooklessPass(seen, now)
	}
	if s := statusOf(t, d, task.ID); s != store.StatusRunning {
		t.Fatalf("a screen drawing again is %s, wanted running", s)
	}
}

func TestAHooklessCardLeftWaitingByAReportWorksAgainOnlyAfterItSettles(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	piRow(t, d)
	task, r, _ := hooklessCard(t, d, "t-pi")
	if err := d.st.SetStatusBecause(task.ID, store.StatusNeedsInput, "blocked"); err != nil {
		t.Fatal(err)
	}
	seen := map[string]*hooklessSeen{}
	now := time.Now()
	for i := 0; i < hooklessBusyTicks+1; i++ {
		now = now.Add(time.Second)
		r.lastOut.Store(now.UnixNano())
		d.hooklessPass(seen, now)
	}
	if s := statusOf(t, d, task.ID); s != store.StatusNeedsInput {
		t.Fatalf("output trailing the report moved the card to %s", s)
	}
	for i := 0; time.Duration(i)*hooklessEvery < hooklessQuiet; i++ {
		now = now.Add(hooklessEvery)
		r.lastOut.Store(now.UnixNano())
		d.hooklessPass(seen, now)
	}
	if s := statusOf(t, d, task.ID); s != store.StatusRunning {
		t.Fatalf("a turn that began before the screen settled left the card %s, wanted running", s)
	}
	if err := d.st.SetStatusBecause(task.ID, store.StatusNeedsInput, "blocked"); err != nil {
		t.Fatal(err)
	}
	now = now.Add(hooklessQuiet)
	d.hooklessPass(seen, now)
	for i := 0; i < hooklessBusyTicks; i++ {
		now = now.Add(time.Second)
		r.lastOut.Store(now.UnixNano())
		d.hooklessPass(seen, now)
	}
	if s := statusOf(t, d, task.ID); s != store.StatusRunning {
		t.Fatalf("a later working turn left the card %s, wanted running", s)
	}
}

func TestAKeyEchoIsNotWork(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	piRow(t, d)
	task, r, _ := hooklessCard(t, d, "t-pi")
	seen := map[string]*hooklessSeen{}
	now := time.Now()
	d.hooklessPass(seen, now.Add(time.Hour))
	now = now.Add(time.Hour)
	for i := 0; i < hooklessBusyTicks+1; i++ {
		now = now.Add(time.Second)
		r.typeMu.Lock()
		r.lastTyped = now
		r.typeMu.Unlock()
		r.lastOut.Store(now.Add(10 * time.Millisecond).UnixNano())
		d.hooklessPass(seen, now)
	}
	if s := statusOf(t, d, task.ID); s != store.StatusNeedsInput {
		t.Fatalf("typing moved the card to %s", s)
	}
}

func TestACardWithHooksIsLeftToThem(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	piRow(t, d)
	claude, _, _ := hooklessCard(t, d, "claude")
	heard, _, _ := hooklessCard(t, d, "t-pi")
	d.act.sessionSpoke(heard.ID)
	seen := map[string]*hooklessSeen{}
	d.hooklessPass(seen, time.Now().Add(time.Hour))
	for _, id := range []string{claude.ID, heard.ID} {
		if s := statusOf(t, d, id); s != store.StatusRunning {
			t.Fatalf("a card with hooks was moved to %s", s)
		}
	}
}

func TestARowWithNoPromptArgsTypesItsOpeningPrompt(t *testing.T) {
	t.Parallel()
	h := &store.Harness{ID: "t-aider", Label: "aider", Cmd: "aider", LaunchMode: store.LaunchPTY, Args: []string{"--yes"}}
	args, _, err := runnerArgs(h, "", "fix the bug", "")
	if err != nil {
		t.Fatalf("a pty row with no prompt_args refused a prompt: %v", err)
	}
	if strings.Join(args, " ") != "--yes" {
		t.Fatalf("the prompt reached argv: %q", args)
	}

	d := testDaemon(t)
	piRow(t, d)
	task, r, p := hooklessCard(t, d, "t-pi")
	prompt := "fix the bug"
	r.openingPrompt.Store(&prompt)
	seen := map[string]*hooklessSeen{}
	now := time.Now()
	d.hooklessPass(seen, now.Add(time.Hour))
	if p.written() != "" || statusOf(t, d, task.ID) != store.StatusRunning {
		t.Fatal("typed or waited before the runner drew anything")
	}
	r.lastOut.Store(now.UnixNano())
	d.hooklessPass(seen, now.Add(time.Second))
	if p.written() != "" {
		t.Fatal("typed while the runner was still drawing")
	}
	d.hooklessPass(seen, now.Add(hooklessReady))
	if got := p.written(); !strings.Contains(got, "fix the bug") || !strings.HasSuffix(got, "\r") {
		t.Fatalf("typed %q, wanted the prompt and Enter", got)
	}
	retyped := now.Add(hooklessReady + hooklessQuiet)
	d.hooklessPass(seen, retyped)
	if got := strings.Count(p.written(), "fix the bug"); got != 2 {
		t.Fatalf("typed the prompt %d times, wanted it typed again when the screen never answered", got)
	}
	r.buf.Write([]byte("\x1b[2J Ready for instructions\r\n"))
	r.lastOut.Store(retyped.Add(time.Millisecond).UnixNano())
	d.hooklessPass(seen, retyped.Add(hooklessEvery))
	if r.openingPrompt.Load() == nil {
		t.Fatal("a redraw that never showed the prompt was taken as the prompt landing")
	}
	r.buf.Write([]byte("> fix the\x1b[0m bug\r\n"))
	r.lastOut.Store(retyped.Add(2 * time.Millisecond).UnixNano())
	d.hooklessPass(seen, retyped.Add(hooklessEvery))
	if r.openingPrompt.Load() != nil {
		t.Fatal("the prompt is still owed after the screen answered it")
	}
}
