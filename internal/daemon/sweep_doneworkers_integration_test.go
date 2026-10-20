//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

func agedDone(t *testing.T, d *Daemon, name string, age time.Duration, tags ...string) *store.Task {
	t.Helper()
	c := doneCard(t, d, name, tags...)
	if err := d.st.BackdateActivity(c.ID, age); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestSweepArchivesADoneWorkerPastTheDelayOnly(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	old := agedDone(t, d, "old", 25*time.Hour, OriginAgentTag, SubagentTag)
	young := agedDone(t, d, "young", time.Hour, OriginAgentTag, SubagentTag)
	if err := d.sweepDoneWorkers(); err != nil {
		t.Fatal(err)
	}
	if !archived(t, d, old.ID) {
		t.Error("a done worker past the delay stayed on the board")
	}
	if archived(t, d, young.ID) {
		t.Error("a done worker younger than the delay was archived")
	}
}

func TestSweepLeavesEveryOtherDoneCard(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	age := 48 * time.Hour
	director := agedDone(t, d, "dir", age, OriginAgentTag, SubagentTag, DirectorTag)
	orchestrator := agedDone(t, d, "orch", age, OrchestratorTag)
	human := agedDone(t, d, "human", age, SubagentTag)
	pinned := agedDone(t, d, "pin", age, OriginAgentTag, SubagentTag)
	if err := d.st.SetPinned(pinned.ID, true); err != nil {
		t.Fatal(err)
	}
	running := agedDone(t, d, "run", age, OriginAgentTag, SubagentTag)
	if err := d.st.SetStatus(running.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if err := d.st.BackdateActivity(running.ID, age); err != nil {
		t.Fatal(err)
	}
	if err := d.sweepDoneWorkers(); err != nil {
		t.Fatal(err)
	}
	for name, c := range map[string]*store.Task{"director": director, "orchestrator": orchestrator,
		"human-launched": human, "pinned": pinned, "running": running} {
		if archived(t, d, c.ID) {
			t.Errorf("the %s card was archived", name)
		}
	}
}

func TestSweepDoneWorkersHonoursTheSetting(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	c := agedDone(t, d, "w", 2*time.Hour, OriginAgentTag, SubagentTag)
	if err := d.st.SetSetting(SettingArchiveWorkersAfter, "off"); err != nil {
		t.Fatal(err)
	}
	if err := d.sweepDoneWorkers(); err != nil {
		t.Fatal(err)
	}
	if archived(t, d, c.ID) {
		t.Fatal("archived while off")
	}
	if err := d.st.SetSetting(SettingArchiveWorkersAfter, "3600"); err != nil {
		t.Fatal(err)
	}
	if err := d.sweepDoneWorkers(); err != nil {
		t.Fatal(err)
	}
	if !archived(t, d, c.ID) {
		t.Fatal("not archived past a one hour delay")
	}
}

func TestArchiveWorkersAndTheSweepSelectTheSameSet(t *testing.T) {
	t.Parallel()
	d := testDaemon(t)
	agedDone(t, d, "w1", 48*time.Hour, OriginAgentTag, SubagentTag)
	agedDone(t, d, "w2", 48*time.Hour, OriginAgentTag, SubagentTag)
	agedDone(t, d, "dir", 48*time.Hour, OriginAgentTag, SubagentTag, DirectorTag)
	agedDone(t, d, "human", 48*time.Hour, SubagentTag)
	swept, _, err := d.doneWorkers(time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	res, err := d.ArchiveWorkers(true)
	if err != nil {
		t.Fatal(err)
	}
	if len(swept) != 2 || len(res.Archived) != len(swept) {
		t.Fatalf("sweep selects %d, archive-workers %d, want both 2", len(swept), len(res.Archived))
	}
}
