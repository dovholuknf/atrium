//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The forwarder tells the hub which branch a card was launched on and who handed it over: its launcher, then the
// earlier cards that launcher launched on that same branch.
func TestPushKinNamesTheLauncherAndEarlierSiblingsOnTheSameBranch(t *testing.T) {
	d := testDaemon(t)
	launcher := peerCard(t, d, "orchestrator")
	launch := func(name, branch string) *store.Task {
		t.Helper()
		time.Sleep(5 * time.Millisecond) // wait: spaces the launches' stamps so earlier siblings sort first
		w := peerCard(t, d, name)
		if err := d.st.SetLineage(w.ID, "orchestrator", launcher.ID); err != nil {
			t.Fatal(err)
		}
		if err := d.st.SetLaunchBranch(w.ID, branch); err != nil {
			t.Fatal(err)
		}
		w, err := d.st.Get(w.ID)
		if err != nil {
			t.Fatal(err)
		}
		if made, err := d.st.CreateWorkItem(w, store.NewWorkItem{Brief: name}); err != nil || !made {
			t.Fatalf("work item: %v %v", made, err)
		}
		return w
	}
	a := launch("a", "claude/x")
	launch("b", "claude/y")
	c := launch("c", "claude/x")

	branch, kin := d.PushKin(c.ID)
	if branch != "claude/x" || strings.Join(kin, ",") != launcher.ID+","+a.ID {
		t.Fatalf("pushKin(c) = %q %v, want claude/x [%s %s]", branch, kin, launcher.ID, a.ID)
	}
	if branch, kin := d.PushKin(a.ID); branch != "claude/x" || strings.Join(kin, ",") != launcher.ID {
		t.Fatalf("pushKin(a) = %q %v, want claude/x [%s]", branch, kin, launcher.ID)
	}
	// A card that was not launched on a branch sends nothing.
	if branch, kin := d.PushKin(launcher.ID); branch != "" || kin != nil {
		t.Fatalf("pushKin(launcher) = %q %v", branch, kin)
	}
}
