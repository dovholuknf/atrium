//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

func finishedWorker(t *testing.T, d *Daemon, name, launcher string) *store.Task {
	t.Helper()
	w := cardFor(t, d, name)
	l := peerCard(t, d, launcher)
	if err := d.st.SetLauncher(w.ID, l.WireName, l.ID); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetResumeID(w.ID, "conv-1"); err != nil {
		t.Fatal(err)
	}
	return w
}

// A wake never starts the next runner while the earlier one is still registered.
func TestAWakeEndsTheEarlierRunnerBeforeStartingTheNext(t *testing.T) {
	d := testDaemon(t)
	w := finishedWorker(t, d, "worker-a", "boss-a")
	early, _ := typedRunner(t, d, w.ID)
	if err := d.st.SetStatus(w.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	if err := d.st.AppendEvent(w.ID, store.EventExited, map[string]any{"by": "session hook"}); err != nil {
		t.Fatal(err)
	}
	go func() {
		time.Sleep(50 * time.Millisecond) // wait: poll until the woken runner tree has gone
		early.exitOnce.Do(func() { close(early.done) })
		d.sup.removeRunner(early)
	}()
	d.wakeLaunch = func(req shared.LaunchRequest) (*store.Task, error) {
		if d.sup.get(w.ID) != nil {
			t.Error("the earlier runner was still registered when the next one started")
		}
		typedRunner(t, d, w.ID)
		return d.st.Get(w.ID)
	}
	if err := d.wakeGone(w.ID, "message"); err != nil {
		t.Fatal(err)
	}
}

// One launcher cannot wake its finished workers without end.
func TestALauncherWakingItsFinishedWorkersIsCapped(t *testing.T) {
	d := testDaemon(t)
	d.wakeLimit.limit = 2
	w := finishedWorker(t, d, "worker-b", "boss-b")
	launched := 0
	d.wakeLaunch = func(req shared.LaunchRequest) (*store.Task, error) {
		launched++
		return d.st.Get(w.ID)
	}
	var err error
	for i := 0; i < 3; i++ {
		d.sup.remove(w.ID)
		if e := d.st.SetStatus(w.ID, store.StatusDone); e != nil {
			t.Fatal(e)
		}
		err = d.wakeGone(w.ID, "message")
		if i < 2 && err != nil {
			t.Fatalf("wake %d refused under the cap: %v", i+1, err)
		}
	}
	if err == nil || !strings.Contains(err.Error(), "which is the limit") {
		t.Fatalf("third wake: %v, want a refusal naming the limit", err)
	}
	if launched != 2 {
		t.Fatalf("launched %d, want 2", launched)
	}
}
