//go:build integration

package daemon

import (
	"os"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

const realIdleFrame = "\x1b[H\x1b[2J● Looked at the tests… (3 files) and the diff is small.\r\n\r\n" + frameRule +
	"\r\n❯ \x1b[2mTry \"fix lint errors\"\x1b[0m\r\n" + frameRule + "\r\n  ⏵⏵ auto mode on (shift+tab to cycle) · ← for agents\r\n"

func TestASayReachesAnIdleRunnerAfterADeployRestartWithoutForce(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	dir, _ := hostFixture(t)
	d1 := daemonAt(t, dir)
	task := cardAt(t, d1, "qa", t.TempDir())
	peerCard(t, d1, "boss")
	sh, args := someShell(t)
	if _, err := d1.spawnPTY(task.ID, sh, append(args, "echo started; sleep 120"), t.TempDir(), os.Environ()); err != nil {
		t.Fatal(err)
	}
	hostWait(t, 30*time.Second, "the runner's first output", func() bool {
		r1 := d1.sup.get(task.ID)
		return r1 != nil && ringHas(r1, "started")
	})
	if err := d1.st.SetStatus(task.ID, store.StatusNeedsInput); err != nil {
		t.Fatal(err)
	}

	d2 := restarted(t, d1, dir)
	d2.hk.backoffSteps = []time.Duration{100 * time.Millisecond}
	d2.reattachRuns()
	r2 := d2.sup.get(task.ID)
	if r2 == nil || !r2.reattached.Load() {
		t.Fatalf("the run was not reattached: %+v", r2)
	}
	t.Cleanup(func() { d2.pending.stopAll() })
	hostWait(t, 30*time.Second, "the replayed output", func() bool { return ringHas(r2, "started") })
	_, _ = r2.buf.Write([]byte(realIdleFrame))
	r2.lastOut.Store(time.Now().Add(-time.Minute).UnixNano())

	for _, text := range []string{"first", "second"} {
		if code, out := forceSayVia(t, d2, task.ID, `{"text":"`+text+`","from":"boss"}`); code != 200 || out["delivered"] == "terminal" {
			t.Fatalf("before the gate is read the say should be held: %d %v", code, out)
		}
	}
	if err := d2.watchLooksIdle(time.Now()); err != nil {
		t.Fatal(err)
	}
	if !r2.peerGateOpen() {
		t.Fatal("the gate stayed shut on a reattached idle runner")
	}
	hostWait(t, 30*time.Second, "both held says to be typed", func() bool {
		p, err := d2.st.PendingMessages(task.ID)
		return err == nil && len(p) == 0
	})
	if code, out := forceSayVia(t, d2, task.ID, `{"text":"third","from":"boss"}`); code != 200 || out["delivered"] != "terminal" {
		t.Fatalf("a say after the gate opened was not typed: %d %v", code, out)
	}
}

func TestAReattachedRunnerWithADraftOrAHintKeepsItsGateShut(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	draft := "\x1b[H\x1b[2J● done.\r\n\r\n" + frameRule + "\r\n❯ half a sentence\r\n" + frameRule + "\r\n  ⏵⏵ auto mode on\r\n"
	_, r := reattachedCard(t, d, "claude", store.StatusNeedsInput, draft, time.Minute)
	_ = d.watchLooksIdle(time.Now())
	if r.peerGateOpen() {
		t.Fatal("the gate opened over typed text")
	}
	d2 := testDaemon(t)
	_, w := reattachedCard(t, d2, "claude", store.StatusNeedsInput, workingFrame, time.Minute)
	_ = d2.watchLooksIdle(time.Now())
	if w.peerGateOpen() {
		t.Fatal("the gate opened under an interrupt hint")
	}
}
