//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// launchedCycler is a launched worker with a terminal, between turns, that its launcher prompted and that has
// reported since. It is where a resident worker sits when its context is cycled.
func launchedCycler(t *testing.T, d *Daemon) (launcher, worker *store.Task, f *fakePTY) {
	t.Helper()
	launcher, worker = launchedPair(t, d)
	if err := d.st.SetSetting(store.SettingContextHandoffDir, t.TempDir()); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(worker.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}
	_, f = typedRunner(t, d, worker.ID)
	t.Cleanup(func() { d.pending.stopAll(); d.Nctx.StopAll() })
	return launcher, worker, f
}

// cycleToWake runs a cycle through its wake and the chip going.
func cycleToWake(t *testing.T, d *Daemon, worker *store.Task, f *fakePTY) {
	t.Helper()
	ncToClear(t, d, worker, f)
	ncTurnEnds(d, worker.ID)
	d.Wake.SawSession(worker.ID, time.Now())
	until(t, "the wake prompt", func() bool { return strings.Contains(f.written(), "and continue.") })
	until(t, "the chip to go", func() bool { return d.NewContextFor(worker.ID) == nil })
}

// Cycling a worker's context opens no report debt and ends in no silent-stop notice. The launcher's last say went
// into the old context, so it is handed back after the wake, and that repeat is not a new ask.
func TestCyclingAWorkersContextOwesNothingAndIsNoSilentStop(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	launcher, worker, f := launchedCycler(t, d)

	// The launcher's say reached the old context, and the worker answered it.
	d.noteInContext(worker.ID, launcher.WireName, "do the errand", "")
	if err := d.st.MarkReported(worker.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := d.st.Get(worker.ID)
	if before.OwesReport() {
		t.Fatal("the setup is wrong: the worker should have reported")
	}

	cycleToWake(t, d, worker, f)

	// The handed back say is queued behind the wake and still arrives, as a repeat.
	until(t, "the repeat to be queued or typed", func() bool {
		return strings.Contains(f.written(), "do the errand") || len(pendingFrom(t, d, worker.ID)) > 0
	})
	after, err := d.st.Get(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.OwesReport() || after.PromptKey() != before.PromptKey() {
		t.Fatalf("the cycle opened report debt: owed_at %v before, %v after", before.OwedAt, after.OwedAt)
	}

	// The fresh context ends its turn without saying anything.
	stopSilentlyByID(t, d, worker.ID)
	if got := pendingFrom(t, d, launcher.ID); len(got) != 0 {
		t.Fatalf("the launcher was told of a silent stop after a new context: %q", got[0].Text)
	}
}

// A cycle does not weaken silent-stop for a worker that stopped after its launcher's own prompt, with a report owed.
func TestACycledWorkerThatOwesStillGetsASilentStop(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	launcher, worker, f := launchedCycler(t, d)

	cycleToWake(t, d, worker, f)
	// The launcher prompts the fresh context, and it ends its turn with nothing said.
	time.Sleep(10 * time.Millisecond) // wait: the prompt must land after the cycle, no event marks it
	prompt(t, d, worker.ID)
	stopSilently(t, d, "worker")
	if got := pendingFrom(t, d, launcher.ID); len(got) != 1 || !strings.Contains(got[0].Text, "ended its turn without reporting") {
		t.Fatalf("the launcher has %d messages, want one silent-stop notice", len(got))
	}
}

// The wake lists the says the card sent since its launcher last prompted it, marked as delivered and not to be sent
// again, and leaves out one from before that prompt.
func TestTheContinuePromptListsTheSaysAlreadySent(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d := testDaemon(t)
	fastNewContext(t, d)
	launcher, worker, f := launchedCycler(t, d)

	say := func(text string) {
		rec := sayRecordFor(worker.WireName, launcher, sayTrace{}, false, "tell", "", false)
		rec.State, rec.Channel = store.SayDelivered, store.SayViaTerminal
		d.recordSay(rec, text)
	}
	say("STALE-ANSWER-BEFORE-THE-PROMPT")
	time.Sleep(20 * time.Millisecond) // wait: the prompt must land after the stale say, no event marks it
	prompt(t, d, worker.ID)
	time.Sleep(20 * time.Millisecond) // wait: the answers must land after the prompt, no event marks it
	say("ANSWER-ALPHA to the errand")
	say("ANSWER-BETA with\nnewlines")

	cycleToWake(t, d, worker, f)

	got := f.written()
	for _, want := range []string{"ANSWER-ALPHA to the errand", "ANSWER-BETA with newlines", "do NOT resend", "to " + launcher.WireName} {
		if !strings.Contains(got, want) {
			t.Fatalf("the continue prompt lacks %q: %q", want, got)
		}
	}
	if strings.Contains(got, "STALE-ANSWER") {
		t.Fatalf("the continue prompt lists a say from before the launcher's prompt: %q", got)
	}
	if strings.Index(got, "ANSWER-ALPHA") > strings.Index(got, "ANSWER-BETA") {
		t.Fatalf("the says are not in the order they were sent: %q", got)
	}
}

// A card that has sent nothing is told only to read the handoff.
func TestTheContinuePromptIsPlainWhenNothingWasSent(t *testing.T) {
	d := testDaemon(t)
	_, worker, _ := launchedCycler(t, d)
	if got, want := d.NewContextWakeFor(worker.ID, "/h.md"), cycle.NewContextWakeText("/h.md"); got != want {
		t.Fatalf("the wake reads %q, want %q", got, want)
	}
}
