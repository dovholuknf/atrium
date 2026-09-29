package daemon

import (
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-007 stage 1: a resident director is not silently stopped while its own
// workers are outstanding, and the board's STUCK mark agrees with the notice.

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
	d.sup.runners[id] = &runner{}
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

func TestDirectorAllWorkersEndedIsSilent(t *testing.T) {
	d := testDaemon(t)
	orch, director, worker := directorRig(t, d)
	liveRunner(d, worker.ID)
	stopTurn(t, d, "director")
	if n := len(pendingFrom(t, d, orch.ID)); n != 0 {
		t.Fatalf("%d notices with a live worker", n)
	}
	endRunner(d, worker.ID)
	stuckAgrees(t, d, director.ID, true)
	stopTurn(t, d, "director")
	stopTurn(t, d, "director")
	if n := len(pendingFrom(t, d, orch.ID)); n != 1 {
		t.Fatalf("%d notices once every worker ended, want one", n)
	}
}

func TestDirectorWithNoWorkersIsSilent(t *testing.T) {
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
	if n := len(pendingFrom(t, d, orch.ID)); n != 1 {
		t.Fatalf("%d notices, want one", n)
	}
	stuckAgrees(t, d, director.ID, true)
}

func TestWorkerSilentStopUnchanged(t *testing.T) {
	d := testDaemon(t)
	directorRig(t, d)
	worker, err := d.st.GetByWireName(d.st.Qualify("worker"))
	if err != nil {
		t.Fatal(err)
	}
	liveRunner(d, worker.ID)
	prompt(t, d, worker.ID)
	stopTurn(t, d, "worker")
	if n := len(pendingFrom(t, d, worker.SpawnedByID)); n != 1 {
		t.Fatalf("the worker's launcher got %d notices, want one", n)
	}
	stuckAgrees(t, d, worker.ID, true)
}
