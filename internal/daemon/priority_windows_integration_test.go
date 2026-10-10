//go:build integration && windows

package daemon

import (
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"

	"golang.org/x/sys/windows"
)

func classOf(t *testing.T, pid uint32) uint32 {
	t.Helper()
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		t.Fatalf("open %d: %v", pid, err)
	}
	defer windows.CloseHandle(h)
	c, err := windows.GetPriorityClass(h)
	if err != nil {
		t.Fatalf("class of %d: %v", pid, err)
	}
	return c
}

// defaultClass is the class a child of this test process starts at when
// nothing raises it. CreateProcess hands a child its parent's class only when
// that class is idle or below normal, and a CI runner runs tests below normal.
func defaultClass(t *testing.T) uint32 {
	t.Helper()
	switch own := classOf(t, uint32(os.Getpid())); own {
	case windows.IDLE_PRIORITY_CLASS, windows.BELOW_NORMAL_PRIORITY_CLASS:
		return own
	}
	return windows.NORMAL_PRIORITY_CLASS
}

func spawnSleeper(t *testing.T, d *Daemon, name string) (uint32, map[uint32]bool) {
	t.Helper()
	dir := t.TempDir()
	task := cardAt(t, d, name, dir)
	shell := "pwsh.exe"
	if _, err := exec.LookPath(shell); err != nil {
		t.Skipf("no powershell to run: %v", err)
	}
	hostsBefore := consoleHosts()
	pid, err := d.spawnPTY(task.ID, shell,
		[]string{"-NoProfile", "-NoLogo", "-Command", "Start-Sleep -Seconds 60"}, dir, os.Environ())
	if err != nil {
		t.Fatalf("a runner must start: %v", err)
	}
	r := d.sup.get(task.ID)
	t.Cleanup(func() {
		r.closePTY()
		_ = r.cmd.Process.Kill()
		select {
		case <-r.done:
		case <-time.After(5 * time.Second):
		}
	})
	fresh := map[uint32]bool{}
	for h := range consoleHosts() {
		if !hostsBefore[h] {
			fresh[h] = true
		}
	}
	return uint32(pid), fresh
}

func TestRunnerAndConsoleHostAreRaised(t *testing.T) {
	d := testDaemon(t)
	pid, hosts := spawnSleeper(t, d, "raised")
	if len(hosts) == 0 {
		t.Fatal("found no console host child of the room for the pty")
	}
	if got := classOf(t, pid); got != windows.ABOVE_NORMAL_PRIORITY_CLASS {
		t.Errorf("runner class = %#x, want above normal", got)
	}
	for h := range hosts {
		if got := classOf(t, h); got != windows.ABOVE_NORMAL_PRIORITY_CLASS {
			t.Errorf("console host %d class = %#x, want above normal", h, got)
		}
	}
}

func TestRunnerPriorityNormalLeavesBothAlone(t *testing.T) {
	d := testDaemon(t)
	if err := d.st.SetSetting(store.SettingRunnerPriority, "normal"); err != nil {
		t.Fatal(err)
	}
	want := defaultClass(t)
	pid, hosts := spawnSleeper(t, d, "normal")
	if got := classOf(t, pid); got != want {
		t.Errorf("runner class = %#x, want %#x, the class it was started at", got, want)
	}
	for h := range hosts {
		if got := classOf(t, h); got != want {
			t.Errorf("console host %d class = %#x, want %#x, the class it was started at", h, got, want)
		}
	}
}

// A refused raise is logged and the runner starts anyway.
func TestFailedRaiseDoesNotFailTheSpawn(t *testing.T) {
	old := setPriorityClass
	setPriorityClass = func(windows.Handle, uint32) error { return errors.New("refused") }
	t.Cleanup(func() { setPriorityClass = old })
	d := testDaemon(t)
	want := defaultClass(t)
	pid, _ := spawnSleeper(t, d, "refused")
	if got := classOf(t, pid); got != want {
		t.Errorf("runner class = %#x, want %#x since the raise was refused", got, want)
	}
}
