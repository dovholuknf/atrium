package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func doneCard(t *testing.T, d *Daemon, name string, tags ...string) *store.Task {
	t.Helper()
	task, _, err := d.st.Register(store.Observed{WireName: name, Worktree: "/tmp/" + name, Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetTags(task.ID, tags); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	return task
}

func archived(t *testing.T, d *Daemon, id string) bool {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return got.ArchivedAt != nil
}

func TestArchiveWorkersTakesOnlyDoneWorkers(t *testing.T) {
	d := testDaemon(t)
	worker := doneCard(t, d, "w1", OriginAgentTag, SubagentTag)
	director := doneCard(t, d, "dir", OriginAgentTag, SubagentTag, DirectorTag)
	human := doneCard(t, d, "human", SubagentTag)
	orchestrator := doneCard(t, d, "orch")
	pinned := doneCard(t, d, "pin", OriginAgentTag, SubagentTag)
	if err := d.st.SetPinned(pinned.ID, true); err != nil {
		t.Fatal(err)
	}
	running := doneCard(t, d, "run", OriginAgentTag, SubagentTag)
	if err := d.st.SetStatus(running.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	res, err := d.ArchiveWorkers(false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Archived) != 1 || res.Archived[0].Card != worker.ID {
		t.Fatalf("result = %+v, want only the worker", res)
	}
	if !archived(t, d, worker.ID) {
		t.Error("the worker is still on the board")
	}
	for name, c := range map[string]*store.Task{"director": director, "no origin:agent": human,
		"orchestrator": orchestrator, "pinned": pinned, "running": running} {
		if archived(t, d, c.ID) {
			t.Errorf("the %s card was archived", name)
		}
	}
}

func TestArchiveWorkersDryRunChangesNothing(t *testing.T) {
	d := testDaemon(t)
	worker := doneCard(t, d, "w1", OriginAgentTag, SubagentTag)
	res, err := d.ArchiveWorkers(true)
	if err != nil {
		t.Fatal(err)
	}
	if !res.DryRun || len(res.Archived) != 1 {
		t.Fatalf("result = %+v, want the worker listed", res)
	}
	if archived(t, d, worker.ID) {
		t.Fatal("a dry run archived a card")
	}
}

func TestArchiveWorkersLeavesAFixtureCard(t *testing.T) {
	d := testDaemon(t)
	card := doneCard(t, d, "fx", OriginAgentTag, SubagentTag)
	if _, err := d.st.SaveFixture(&store.Fixture{ID: "f1", Harness: "shell", Cwd: "/tmp", Enabled: true,
		TaskID: card.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := d.ArchiveWorkers(false); err != nil {
		t.Fatal(err)
	}
	if archived(t, d, card.ID) {
		t.Fatal("a fixture's card was archived")
	}
}
