//go:build integration

package daemon

import (
	"testing"

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
