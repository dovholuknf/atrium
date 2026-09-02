package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/hub"
	"github.com/dovholuknf/atrium/internal/store"
)

// Moving a card out of a waiting state has to answer what it was holding. A
// request left pending keeps its agent frozen with nobody coming, and stays in
// the queue to be approved later against a situation already walked away from.
func TestCancelPendingReleasesTheAgent(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	task, _, err := st.Register(store.Observed{WireName: "shelf-me", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := st.RecordPermission(task.ID, "Bash", "rm -rf ./build", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}

	n, err := d.CancelPending(task.ID, "shelved")
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("cancelled %d requests, want 1", n)
	}

	after, err := st.GetPermission(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.DecidedAt == nil {
		t.Fatal("the request is still pending, so the agent is still frozen")
	}
	if after.Decision != "block" {
		t.Fatalf("decision is %q, want block", after.Decision)
	}
	if after.Reason == "" {
		t.Fatal("a block with no reason tells the agent nothing")
	}

	// And it is gone from the queue, so it cannot be answered again later.
	pending, err := st.PendingPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("%d requests still answerable after the card moved", len(pending))
	}
}

// A shelved card is a standing no. Without this an agent asks, gets nothing,
// and freezes behind a card the operator has deliberately stopped looking at.
func TestShelvedTaskBlocksNewRequests(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	task, _, err := st.Register(store.Observed{WireName: "shelved-one", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}

	_, auto, err := d.onPermRequest(hub.PermissionRequest{
		Agent: "shelved-one", Tool: "Bash", Command: "go build ./...",
	})
	if err != nil {
		t.Fatal(err)
	}
	if auto == nil {
		t.Fatal("a shelved task left the request pending, so the agent waits on a card nobody is watching")
	}
	if auto.Decision != "block" {
		t.Fatalf("decision is %q, want block", auto.Decision)
	}
	if auto.Reason == "" {
		t.Fatal("the agent was refused without being told why")
	}

	// Nothing was added to the queue for a card the operator put down.
	pending, err := st.PendingPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 0 {
		t.Fatalf("a shelved task queued %d requests", len(pending))
	}
}

// Unshelving restores normal behaviour, or shelving would be a trap.
func TestUnshelvedTaskAsksAgain(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	task, _, err := st.Register(store.Observed{WireName: "back-again", Worktree: "d:/w", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}
	if err := st.SetStatus(task.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}

	_, auto, err := d.onPermRequest(hub.PermissionRequest{
		Agent: "back-again", Tool: "Bash", Command: "something with no rule",
	})
	if err != nil {
		t.Fatal(err)
	}
	if auto != nil {
		t.Fatalf("an unshelved task auto-answered with %q", auto.Decision)
	}
	pending, err := st.PendingPermissions()
	if err != nil {
		t.Fatal(err)
	}
	if len(pending) != 1 {
		t.Fatalf("want the request queued for a human, got %d pending", len(pending))
	}
}
