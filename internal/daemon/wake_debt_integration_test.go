//go:build integration

package daemon

import (
	"github.com/dovholuknf/atrium/internal/daemon/cycle"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A restart wake typed into a launched worker is atrium's line, not its launcher's, so it opens no report debt and
// the turn it starts ends in no silent-stop notice.
func TestARestartWakeOwesNothingAndIsNoSilentStop(t *testing.T) {
	d := testDaemon(t)
	t.Cleanup(func() { d.pending.stopAll() })
	launcher, worker := launchedPair(t, d)
	_, f := typedRunner(t, d, worker.ID)
	if err := d.st.MarkReported(worker.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := d.st.Get(worker.ID)

	w, _, err := d.QueueWake(worker.ID, "we up", launcher.WireName)
	if err != nil {
		t.Fatal(err)
	}
	r := d.sup.get(worker.ID)
	r.started = w.QueuedAt.Add(time.Second)
	started := r.started.Add(time.Second)
	d.Wake.SawSession(worker.ID, started)
	r.typeMu.Lock()
	r.lastTyped = time.Now().Add(-peerGateIdle - time.Second)
	r.typeMu.Unlock()
	d.WakeTick(started.Add(cycle.WakeSettle))
	if !strings.Contains(f.written(), "we up") {
		t.Fatalf("the wake was not typed: %q", f.written())
	}

	after, _ := d.st.Get(worker.ID)
	if after.OwesReport() || after.PromptKey() != before.PromptKey() {
		t.Fatalf("the restart wake opened report debt: owed_at %v before, %v after", before.OwedAt, after.OwedAt)
	}
	stopSilentlyByID(t, d, worker.ID)
	if got := pendingFrom(t, d, launcher.ID); len(got) != 0 {
		t.Fatalf("the launcher was told of a silent stop after a restart wake: %q", got[0].Text)
	}
}

// The handoff line a resumed (unparked) worker is queued opens no report debt either, delivered or not.
func TestAResumedWorkersHandoffLineOwesNothingAndIsNoSilentStop(t *testing.T) {
	d := testDaemon(t)
	launcher, worker := launchedPair(t, d)
	if err := d.st.MarkReported(worker.ID); err != nil {
		t.Fatal(err)
	}
	before, _ := d.st.Get(worker.ID)
	if err := d.st.SetSetting(store.SettingParkHandoffPrefix+worker.ID, "handoff.md|600"); err != nil {
		t.Fatal(err)
	}

	d.queueHandoffWake(worker.ID)
	msgs := pendingFrom(t, d, worker.ID)
	if len(msgs) != 1 || !strings.Contains(msgs[0].Text, "handoff.md") {
		t.Fatalf("the handoff line was not queued: %+v", msgs)
	}
	if err := d.markDelivered(worker.ID, "terminal", []string{msgs[0].ID}); err != nil {
		t.Fatal(err)
	}
	d.notePeerTyped(worker.ID, msgs[0].FromPeer, msgs[0].Text, "", "typed and sent when the resumed session was ready")

	after, _ := d.st.Get(worker.ID)
	if after.OwesReport() || after.PromptKey() != before.PromptKey() {
		t.Fatalf("the resume opened report debt: owed_at %v before, %v after", before.OwedAt, after.OwedAt)
	}
	stopSilentlyByID(t, d, worker.ID)
	if got := pendingFrom(t, d, launcher.ID); len(got) != 0 {
		t.Fatalf("the launcher was told of a silent stop after a resume: %q", got[0].Text)
	}
}
