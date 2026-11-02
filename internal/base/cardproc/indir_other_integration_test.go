//go:build integration && !windows

package cardproc

import (
	"os/exec"
	"syscall"
	"testing"
	"time"
)

func TestStopInDirEndsAProcessThatLeftItsParentInsideTheDir(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("sleep", "300")
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	if _, ok := InDir(dir)[cmd.Process.Pid]; !ok {
		t.Fatalf("InDir missed %d", cmd.Process.Pid)
	}
	if left := WaitGone(StopInDir(dir), 5*time.Second); len(left) > 0 {
		t.Fatalf("still running: %v", left)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("sleep outlived StopInDir")
	}
	if len(InDir(dir)) != 0 {
		t.Errorf("still found: %v", InDir(dir))
	}
}
