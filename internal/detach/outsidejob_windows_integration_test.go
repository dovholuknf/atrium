//go:build integration && windows

package detach

import (
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// The pid answered is the program's, not the cmd.exe WMI wraps it in.
func TestStartOutsideJobAnswersTheProgramNotCmd(t *testing.T) {
	p, err := startOutsideJob(`C:\Windows\System32\PING.EXE`, []string{"-n", "20", "127.0.0.1"}, nil)
	if err != nil {
		t.Skipf("WMI is not available here: %v", err)
	}
	t.Cleanup(func() { _ = p.Kill() })
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(p.Pid), "/NH").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(out)), "ping.exe") {
		t.Fatalf("pid %d is %q, want ping.exe", p.Pid, strings.TrimSpace(string(out)))
	}
}