//go:build integration && windows

package detach

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/base/testwait"
)

func TestStartOutsideJobAnswersTheProgramAndWritesItsLog(t *testing.T) {
	requireWMI(t)
	log := filepath.Join(t.TempDir(), "out.log")
	since := time.Now()
	p, err := startOutsideJob(`C:\Windows\System32\PING.EXE`, []string{"-n", "30", "127.0.0.1"}, log)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("started after %s", time.Since(since))
	t.Cleanup(func() { _ = p.Kill() })
	out, err := exec.Command("tasklist", "/FI", "PID eq "+strconv.Itoa(p.Pid), "/NH").Output()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.ToLower(string(out)), "ping.exe") {
		t.Fatalf("pid %d is %q, want ping.exe", p.Pid, strings.TrimSpace(string(out)))
	}
	var data []byte
	if !testwait.PollEvery(50*time.Millisecond, 10*time.Second, func() bool {
		data, _ = os.ReadFile(log)
		return strings.Contains(string(data), "127.0.0.1")
	}) {
		t.Fatalf("ping's output never reached the log, which has %q", data)
	}
}

func TestStartOutsideJobSaysWhenCmdCannotOpenAHeldLog(t *testing.T) {
	requireWMI(t)
	log := filepath.Join(t.TempDir(), "held.log")
	f, err := os.OpenFile(log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	since := time.Now()
	p, err := startOutsideJob(`C:\Windows\System32\PING.EXE`, []string{"-n", "2", "127.0.0.1"}, log)
	took := time.Since(since)
	if p != nil {
		_ = p.Kill()
	}
	if err == nil {
		t.Fatalf("a held log was opened by cmd after all, in %s", took)
	}
	if !strings.Contains(err.Error(), "cannot open the log") {
		t.Fatalf("the error does not name the cause: %v", err)
	}
	t.Logf("named the cause after %s", took)
	if took > 4*time.Second {
		t.Fatalf("took %s to notice cmd was gone", took)
	}
}

func requireWMI(t *testing.T) {
	t.Helper()
	if _, err := exec.LookPath(powershellPath()); err != nil {
		t.Fatalf("no powershell.exe to drive WMI with: %v", err)
	}
	probe, err := startOutsideJob(`C:\Windows\System32\PING.EXE`, []string{"-n", "30", "127.0.0.1"}, "")
	if err != nil {
		if strings.Contains(err.Error(), "exit status 3") {
			t.Skipf("WMI refuses Win32_Process.Create for this user, so nothing can start outside a job here: %v", err)
		}
		t.Fatalf("the WMI start failed: %v", err)
	}
	_ = probe.Kill()
}
