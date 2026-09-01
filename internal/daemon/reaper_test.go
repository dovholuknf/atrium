package daemon

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// Liveness is an operating system question, so it must be answerable without
// touching the runner at all.
func TestProcessAliveOnSelf(t *testing.T) {
	if !processAlive(os.Getpid()) {
		t.Fatal("this process reported itself as dead")
	}
	if processAlive(0) || processAlive(-1) {
		t.Fatal("a nonsense pid reported as alive")
	}
}

func TestProcessAliveAfterExit(t *testing.T) {
	name, args := "true", []string{}
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/c", "exit"}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn a throwaway process: %v", err)
	}
	pid := cmd.Process.Pid
	if !processAlive(pid) && cmd.ProcessState == nil {
		// Racy on a very fast exit, so only a hard failure counts here.
		t.Log("process exited before the first check, which is fine")
	}
	if err := cmd.Wait(); err != nil {
		t.Logf("child exited with %v", err)
	}
	if processAlive(pid) {
		t.Fatalf("pid %d still reports alive after the process exited", pid)
	}
}

// The reaper marks a card dead when its process is gone, and leaves alone any
// card whose process it does not know about. Not knowing is not the same as
// being dead.
func TestReaperMarksDeadOnlyWhenPidIsKnownAndGone(t *testing.T) {
	d, _, cancel, errCh := startDaemon(t)
	defer func() {
		cancel()
		<-errCh
	}()
	st := d.Store()

	// A card with a pid that has exited.
	name, args := "true", []string{}
	if runtime.GOOS == "windows" {
		name, args = "cmd.exe", []string{"/c", "exit"}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Start(); err != nil {
		t.Skipf("could not spawn a throwaway process: %v", err)
	}
	deadPid := cmd.Process.Pid
	_ = cmd.Wait()

	gone, _, err := st.Register(store.Observed{
		WireName: "gone", Worktree: "d:/gone", Runner: "claude", PID: deadPid,
	})
	if err != nil {
		t.Fatal(err)
	}
	// A card with no pid at all.
	unknown, _, err := st.Register(store.Observed{
		WireName: "unknown", Worktree: "d:/unknown", Runner: "claude",
	})
	if err != nil {
		t.Fatal(err)
	}
	// A card whose process is this test binary, which is very much alive.
	alive, _, err := st.Register(store.Observed{
		WireName: "alive", Worktree: "d:/alive", Runner: "claude", PID: os.Getpid(),
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := d.reapOnce(); err != nil {
		t.Fatal(err)
	}

	check := func(id, want, why string) {
		t.Helper()
		got, err := st.Get(id)
		if err != nil {
			t.Fatal(err)
		}
		if got.Status != want {
			t.Errorf("%s: status is %q, want %q (%s)", got.WireName, got.Status, want, why)
		}
	}
	check(gone.ID, store.StatusDead, "its process exited")
	check(unknown.ID, store.StatusRunning, "an unknown pid must not be assumed dead")
	check(alive.ID, store.StatusRunning, "its process is running")
}
