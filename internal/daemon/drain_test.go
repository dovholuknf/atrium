package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Turning auto mode on with a full queue has to empty it. The switch says
// nothing will stop to ask, and a queue that stays full says otherwise.
func TestDrainApprovesWhatWasAlreadyWaiting(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "waiting", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := d.st.RecordPermission(task.ID, "Bash", "go test ./...", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusNeedsPermission); err != nil {
		t.Fatal(err)
	}

	n, err := d.drainForAuto()
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("drained %d, wanted 1", n)
	}
	got, err := d.st.GetPermission(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Decision != "approve" {
		t.Fatalf("the waiting request was answered %q, wanted approve", got.Decision)
	}
}

// Shelving sits ahead of auto mode in the chain, so the drain must not step
// over it. A card can be shelved after its request was recorded, which is the
// only way a pending request ends up on a shelved card at all.
func TestDrainLeavesAShelvedCardAlone(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	task, _, err := d.st.Register(store.Observed{
		WireName: "put-down", Worktree: "/tmp/atrium-test", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	p, _, err := d.st.RecordPermission(task.ID, "Bash", "rm -rf /", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusShelved); err != nil {
		t.Fatal(err)
	}

	if _, err := d.drainForAuto(); err != nil {
		t.Fatal(err)
	}
	got, err := d.st.GetPermission(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.DecidedAt != nil {
		t.Fatalf("auto mode approved a request on a shelved card: %q", got.Decision)
	}
}
