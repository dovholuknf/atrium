//go:build integration

package daemon

import (
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE DOUBLE PRESS. Two launches onto one dead card, the second arriving while
// the first is still starting, which is two clicks on the resume dialog's
// `launch`. Both answer the same card, with one runner behind it, and neither
// is an error.
func TestASecondLaunchOntoAStartingCardAnswersTheFirst(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	h := slowHarness(t, d)
	task, _, err := d.st.Register(store.Observed{
		WireName: "pressed-twice", Worktree: t.TempDir(), Runner: h,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := d.st.SetStatus(task.ID, store.StatusDead); err != nil {
		t.Fatal(err)
	}

	var wg sync.WaitGroup
	got := make([]*store.Task, 2)
	errs := make([]error, 2)
	for i := range got {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			got[i], errs[i] = d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), TaskID: task.ID})
		}(i)
	}
	wg.Wait()
	defer func() { _ = d.StopRunner(task.ID) }()

	if errs[0] != nil && errs[1] != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", errs[0])
	}
	for i, err := range errs {
		if err != nil {
			t.Fatalf("launch %d of a double press failed: %v", i, err)
		}
		if got[i].ID != task.ID {
			t.Fatalf("launch %d answered card %s, want %s", i, got[i].ID, task.ID)
		}
	}
	if d.sup.get(task.ID) == nil {
		t.Fatal("the card owns no runner after a double press")
	}
}

// Outside the window a launch onto a live card is a request made on purpose,
// and it keeps its refusal.
func TestALaunchOntoALiveCardLongAfterItStartedIsStillRefused(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	h := briefHarness(t, d)
	task, _, err := d.st.Register(store.Observed{
		WireName: "long-running", Worktree: t.TempDir(), Runner: h,
	})
	if err != nil {
		t.Fatal(err)
	}
	d.sup.add(&runner{taskID: task.ID, done: make(chan struct{})})
	defer d.sup.remove(task.ID)

	d.startedAt.Store(task.ID, time.Now().Add(-2*repeatWindow))
	if _, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), TaskID: task.ID}); err == nil {
		t.Fatal("a launch onto a card whose runner has been up for minutes was answered as a repeat")
	}

	d.startedAt.Store(task.ID, time.Now())
	again, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), TaskID: task.ID})
	if err != nil {
		t.Fatalf("a launch inside the window was refused: %v", err)
	}
	if again.ID != task.ID {
		t.Fatalf("a repeat answered card %s, want %s", again.ID, task.ID)
	}
}
