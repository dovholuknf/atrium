//go:build integration

package daemon

import (
	"encoding/json"
	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"strings"
	"testing"
)

// THE ROOM'S HALF OF THE LAUNCH KEY. A launch filed under a key is answered again with the same card, marked as a
// replay, and no second session starts. A different key is a new card, and no key is a launch as it always was.
func TestALaunchKeyReturnsTheCardAlreadyFiledUnderIt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	h := slowHarness(t, d)
	dir := t.TempDir()

	first, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: dir, LaunchKey: "key-one"})
	if err != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", err)
	}
	defer func() { _ = d.StopRunner(first.ID) }()
	if first.LaunchReplay {
		t.Fatal("the first launch was marked a replay")
	}
	if got, err := d.st.GetByLaunchKey("key-one"); err != nil || got.ID != first.ID {
		t.Fatalf("the key is not filed on the card: %+v %v", got, err)
	}
	before, _ := d.st.List()

	again, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: dir, LaunchKey: "key-one"})
	if err != nil {
		t.Fatalf("the repeat failed: %v", err)
	}
	if again.ID != first.ID || !again.LaunchReplay {
		t.Fatalf("the repeat answered %s replay=%v, want %s as a replay", again.ID, again.LaunchReplay, first.ID)
	}
	if after, _ := d.st.List(); len(after) != len(before) {
		t.Fatalf("the repeat filed a card: %d then %d", len(before), len(after))
	}
	if raw, _ := json.Marshal(again); !strings.Contains(string(raw), `"launch_replay":true`) {
		t.Fatalf("the answer does not say it is a replay: %s", raw)
	}

	other, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: t.TempDir(), LaunchKey: "key-two"})
	if err != nil {
		t.Fatalf("a different key was refused: %v", err)
	}
	defer func() { _ = d.StopRunner(other.ID) }()
	if other.ID == first.ID || other.LaunchReplay {
		t.Fatalf("a different key answered %s replay=%v", other.ID, other.LaunchReplay)
	}

	// No key: each launch is its own, as before.
	a, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.StopRunner(a.ID) }()
	b, err := d.Launch(shared.LaunchRequest{Harness: h, Cwd: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.StopRunner(b.ID) }()
	if a.ID == b.ID || a.LaunchReplay || b.LaunchReplay {
		t.Fatalf("launches with no key were joined: %s %s", a.ID, b.ID)
	}
}
