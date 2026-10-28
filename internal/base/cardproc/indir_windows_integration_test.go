//go:build integration && windows

package cardproc

import (
	"os/exec"
	"testing"
	"time"
)

func TestStopInDirEndsAProcessThatLeftItsParentInsideTheDir(t *testing.T) {
	dir := t.TempDir()
	cmd := exec.Command("powershell", "-NoProfile", "-Command", "Start-Sleep 300")
	cmd.Dir = dir
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill() })

	deadline := time.Now().Add(5 * time.Second)
	for _, ok := InDir(dir)[cmd.Process.Pid]; !ok; _, ok = InDir(dir)[cmd.Process.Pid] {
		if time.Now().After(deadline) {
			t.Fatalf("InDir missed %d", cmd.Process.Pid)
		}
		time.Sleep(200 * time.Millisecond) // wait: poll for the process to show under one 5s deadline
	}
	if left := WaitGone(StopInDir(dir), 10*time.Second); len(left) > 0 {
		t.Fatalf("still running: %v", left)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("powershell outlived StopInDir")
	}
	if len(InDir(dir)) != 0 {
		t.Errorf("still found: %v", InDir(dir))
	}
}
