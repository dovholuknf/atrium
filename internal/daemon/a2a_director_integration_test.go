//go:build integration

package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// directorRig is an orchestrator, a director it launched and prompted, and one
// worker the director launched, with the worker's runner faked in.
func directorRig(t *testing.T, d *Daemon) (orch, director, worker *store.Task) {
	t.Helper()
	orch = peerCard(t, d, "orchestrator")
	director = peerCard(t, d, "director")
	if err := d.st.SetTags(director.ID, []string{OriginAgentTag, DirectorTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(director.ID, "orchestrator", orch.ID); err != nil {
		t.Fatal(err)
	}
	worker = peerCard(t, d, "worker")
	if err := d.st.SetTags(worker.ID, []string{OriginAgentTag, SubagentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(worker.ID, "director", director.ID); err != nil {
		t.Fatal(err)
	}
	w, err := d.st.Get(worker.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := d.st.CreateWorkItem(w, store.NewWorkItem{Brief: "a piece of work"}); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, director.ID)
	return orch, director, worker
}

func liveRunner(d *Daemon, id string) {
	d.sup.mu.Lock()
	d.sup.runners[id] = &runner{started: time.Now()}
	d.sup.mu.Unlock()
}

func endRunner(d *Daemon, id string) {
	d.sup.mu.Lock()
	delete(d.sup.runners, id)
	d.sup.mu.Unlock()
}

// stuckAgrees checks the board's mark and the notice keep one definition.
func stuckAgrees(t *testing.T, d *Daemon, id string, wantStuck bool) {
	t.Helper()
	got, err := d.st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	x := d.stuckNow(got, time.Now().Add(time.Hour))
	if (x != nil && x.Source == NoticeSilentStop) != wantStuck {
		t.Fatalf("stuck mark %+v, want stuck=%v", x, wantStuck)
	}
	if _, ok := d.stoppedSilently(got); ok != wantStuck {
		t.Fatalf("stoppedSilently %v, want %v", ok, wantStuck)
	}
}

func TestDirectorWithLiveWorkerNotSilent(t *testing.T) {
	d := testDaemon(t)
	orch, director, worker := directorRig(t, d)
	liveRunner(d, worker.ID)
	stopTurn(t, d, "director")
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("the orchestrator got %d notices while a worker was live", n)
	}
	stuckAgrees(t, d, director.ID, false)

	// A worker that reported done and sits at its prompt is still outstanding.
	if err := d.st.SetStatus(worker.ID, store.StatusDone); err != nil {
		t.Fatal(err)
	}
	stuckAgrees(t, d, director.ID, false)
}

// A director is resident: ending a turn waiting for work is not a stop, with
// workers outstanding or without. See stoppedSilently.
func TestDirectorWhoseWorkersAllEndedIsNotSilent(t *testing.T) {
	d := testDaemon(t)
	orch, director, worker := directorRig(t, d)
	liveRunner(d, worker.ID)
	stopTurn(t, d, "director")
	endRunner(d, worker.ID)
	stuckAgrees(t, d, director.ID, false)
	stopTurn(t, d, "director")
	stopTurn(t, d, "director")
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("%d notices for a director that ended a turn waiting, want none", n)
	}
}

func TestDirectorWithNoWorkersIsNotSilent(t *testing.T) {
	d := testDaemon(t)
	orch := peerCard(t, d, "orchestrator")
	director := peerCard(t, d, "director")
	if err := d.st.SetTags(director.ID, []string{OriginAgentTag, DirectorTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(director.ID, "orchestrator", orch.ID); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, director.ID)
	stopTurn(t, d, "director")
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("%d notices, want none", n)
	}
	stuckAgrees(t, d, director.ID, false)
}

// Without the director tag the same card is a worker owing a report.
func TestUntaggedLauncherSessionStillOwesAndIsSilent(t *testing.T) {
	d := testDaemon(t)
	orch := peerCard(t, d, "orchestrator")
	card := peerCard(t, d, "resident")
	if err := d.st.SetTags(card.ID, []string{OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetLineage(card.ID, "orchestrator", orch.ID); err != nil {
		t.Fatal(err)
	}
	prompt(t, d, card.ID)
	stopTurn(t, d, "resident")
	stuckAgrees(t, d, card.ID, true)
}

func TestWorkerSilentStopUnchanged(t *testing.T) {
	d := testDaemon(t)
	directorRig(t, d)
	worker, err := d.st.GetByWireName(d.st.Qualify("worker"))
	if err != nil {
		t.Fatal(err)
	}
	prompt(t, d, worker.ID)
	// The first stop earns the worker its nudge; the second reaches the launcher.
	// The fake runner comes after the nudge, which would otherwise be typed into it.
	stopTurn(t, d, "worker")
	if _, err := d.takeMessages(worker.ID, "stop"); err != nil {
		t.Fatal(err)
	}
	liveRunner(d, worker.ID)
	d.turnResumed(worker.ID)
	time.Sleep(5 * time.Millisecond)
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, worker.SpawnedByID)); n != 1 {
		t.Fatalf("the worker's launcher got %d notices, want one", n)
	}
	stuckAgrees(t, d, worker.ID, true)
}
