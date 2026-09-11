package daemon

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE MODEL COMES BACK WITH THE CARD.
//
// This is the hazard the work order for choosing a model named, and it is not
// hypothetical: `reopenSaved` rebuilds a launch out of the card, so anything
// not carried across reverts to the runner's default on the next restart. A
// session started on one model and silently moved to another is the failure
// choosing a model exists to prevent, arriving through a different door.
//
// Checked on what `reopen` reads rather than on a spawned process, because what
// goes wrong is a field nobody copied, and no runner has to start for that to
// be true or false.
func TestAReopenedCardComesBackOnItsModel(t *testing.T) {
	d := reopenDaemon(t)

	dir := t.TempDir()
	task, _, err := d.st.Register(store.Observed{
		WireName: "on-a-model", Worktree: dir, Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetModel(task.ID, "claude-fable-5-1"); err != nil {
		t.Fatal(err)
	}
	d.saveReopen([]*runner{{taskID: task.ID, buf: newRing(64, 80)}})

	wanted := d.reopenWanted()
	if len(wanted) != 1 {
		t.Fatalf("expected the one card back, got %d", len(wanted))
	}
	if wanted[0].Model != "claude-fable-5-1" {
		t.Fatalf("the card reopen rebuilds from says its model is %q", wanted[0].Model)
	}
}
