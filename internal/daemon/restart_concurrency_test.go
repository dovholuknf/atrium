package daemon

import (
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// slowHarness registers a runner whose command is a real shell that comes up and
// stays alive, so a launch actually spawns a supervised process rather than
// failing at the spawn. windDown's fallback exit keys (`exit\r\n`) stop it.
func slowHarness(t *testing.T, d *Daemon) string {
	t.Helper()
	cmd := "sh"
	if runtime.GOOS == "windows" {
		cmd = "cmd.exe"
	}
	if _, err := d.st.SaveHarness(store.Harness{
		ID: "slowtest", Label: "slow test", Enabled: true,
		Cmd: cmd, LaunchMode: store.LaunchPTY,
	}); err != nil {
		t.Fatal(err)
	}
	return "slowtest"
}

// launchSlow starts one supervised shell onto a fresh card and returns its id.
// It skips the test rather than failing it when the machine cannot spawn one,
// since that is a property of the box and not of the code under test.
func launchSlow(t *testing.T, d *Daemon) string {
	t.Helper()
	h := slowHarness(t, d)
	task, err := d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir()})
	if err != nil {
		t.Skipf("could not spawn a slow test runner on this machine: %v", err)
	}
	if d.sup.get(task.ID) == nil {
		t.Skip("the slow test runner did not stay up long enough to supervise")
	}
	return task.ID
}

// A launch cannot pass its liveness guard while another operation holds that
// card's launch lock. This is the atomic check-then-spawn region finding #1 is
// about: without the lock, two racers both read "not live" and both spawn onto
// one conversation.
func TestLaunchSerializesOnTheCardLock(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	h := briefHarness(t, d)
	task, _, err := d.st.Register(store.Observed{
		WireName: "locked-card", Worktree: t.TempDir(), Runner: h,
	})
	if err != nil {
		t.Fatal(err)
	}

	unlock := d.launching.lock(launchTaskKey(task.ID))
	done := make(chan struct{})
	go func() {
		// The command does not exist, so this fails as soon as it gets past the
		// lock. What matters is that it cannot get past it while the lock is held.
		_, _ = d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), TaskID: task.ID})
		close(done)
	}()

	select {
	case <-done:
		unlock()
		t.Fatal("a launch onto a card ran while that card's lock was held")
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a launch never proceeded after the card lock was released")
	}
}

// The same, for the resume id. Two launches resuming one conversation must not
// both get through, so a launch waits on the resume lock exactly as it waits on
// the card lock.
func TestLaunchSerializesOnTheResumeLock(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()

	h := briefHarness(t, d)
	resume := "sess-locked"

	unlock := d.launching.lock(launchResumeKey(resume))
	done := make(chan struct{})
	go func() {
		_, _ = d.Launch(LaunchRequest{Harness: h, Cwd: t.TempDir(), Resume: resume})
		close(done)
	}()

	select {
	case <-done:
		unlock()
		t.Fatal("a launch resuming a conversation ran while that resume's lock was held")
	case <-time.After(300 * time.Millisecond):
	}
	unlock()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("a launch never proceeded after the resume lock was released")
	}
}

// Two restarts of one card at the same time serialize instead of racing. Without
// the per-card lock both pass the "is a runner live" guard while the card owns
// no runner, and both spawn a process resuming the same conversation. With it
// they run one after the other, both succeed, and the card ends with exactly one
// live runner.
func TestConcurrentRestartsKeepOneRunner(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	taskID := launchSlow(t, d)

	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i := range errs {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, errs[i] = d.RestartRunner(taskID)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent restart %d failed, which means they did not serialize: %v", i, err)
		}
	}
	if d.sup.get(taskID) == nil {
		t.Fatal("the card owns no runner after two concurrent restarts")
	}
	// Stop the runner before the daemon shuts down, so shutdown does not wind down
	// a shell that would otherwise sit through its grace.
	_ = d.StopRunner(taskID)
}

// The happy path: restarting a live card lands on the SAME card with a runner on
// it, rather than making a second card or leaving the first without one.
func TestRestartRunnerLandsOnTheSameCard(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()

	taskID := launchSlow(t, d)

	started, err := d.RestartRunner(taskID)
	if err != nil {
		t.Fatalf("restarting a live card failed: %v", err)
	}
	if started.ID != taskID {
		t.Fatalf("a restart made a new card %s instead of restarting %s", started.ID, taskID)
	}
	if d.sup.get(taskID) == nil {
		t.Fatal("the card has no runner after a restart")
	}
	_ = d.StopRunner(taskID)
}
