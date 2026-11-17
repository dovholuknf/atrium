//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestARunnerLostWithItsHostComesBackAtStart(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	windDownLike(t, d, task)
	if err := d.st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	// What a killed host leaves: a start nobody filed an exit for, and no wind-down list.
	runID := store.NewRunID()
	if err := d.st.RecordRun(store.PtyRun{RunID: runID, TaskID: task.ID, Kind: store.RunKindRunner}); err != nil {
		t.Fatal(err)
	}

	d.reopenLostRuns()

	if d.sup.get(task.ID) == nil {
		t.Fatal("a card whose runner died with its terminal host was not resumed")
	}
	if run, err := d.st.Run(runID); err != nil || !run.Filed {
		t.Fatalf("the lost start was left unfiled: %+v, %v", run, err)
	}
}

func TestAFinishedCardIsNotResumedForAnOldUnfiledRun(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	t.Parallel()
	d := testDaemon(t)
	task := shellCard(t, d)
	windDownLike(t, d, task)
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	if err := d.st.RecordRun(store.PtyRun{RunID: store.NewRunID(), TaskID: task.ID, Kind: store.RunKindRunner}); err != nil {
		t.Fatal(err)
	}
	d.reopenLostRuns()
	if d.sup.get(task.ID) != nil {
		t.Fatal("a done card was resumed")
	}
}

// The boot sweep runs beside a runner this daemon just started. Its run is
// recorded before the supervisor holds it, and filing it then as lost made the
// real exit skip the throwaway cleanup.
func TestARunStartedSinceBootIsNotFiledAsLost(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	d.bootAt = time.Now()
	task := aThrowaway(t, d)
	runID := store.NewRunID()
	if err := d.st.RecordRun(store.PtyRun{RunID: runID, TaskID: task.ID, Kind: store.RunKindRunner,
		Started: time.Now()}); err != nil {
		t.Fatal(err)
	}

	d.reopenLostRuns()

	if !d.fileExit(runExit{taskID: task.ID, runID: runID, code: 1}) {
		t.Fatal("the boot sweep filed a run this daemon started, so its exit was skipped")
	}
	if _, err := os.Stat(filepath.FromSlash(task.Worktree)); !os.IsNotExist(err) {
		t.Fatalf("%s is still there after the stop", task.Worktree)
	}
	if _, err := d.st.Get(task.ID); err == nil {
		t.Fatal("the card outlived the stop")
	}
}
