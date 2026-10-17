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
)

func TestStartOutsideJobAnswersTheProgramAndWritesItsLog(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skipf("no powershell.exe on PATH: %v", err)
	}
	log := filepath.Join(t.TempDir(), "out.log")
	since := time.Now()
	p, err := startOutsideJob(`C:\Windows\System32\PING.EXE`, []string{"-n", "2", "127.0.0.1"}, log)
	if err != nil {
		if strings.Contains(err.Error(), "exit status 3") {
			t.Skipf("WMI refused to create a process here: %v", err)
		}
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
	deadline := time.Now().Add(10 * time.Second)
	for {
		data, _ := os.ReadFile(log)
		if strings.Contains(string(data), "127.0.0.1") {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("ping's output never reached the log, which has %q", data)
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestStartOutsideJobSaysWhenCmdCannotOpenAHeldLog(t *testing.T) {
	if _, err := exec.LookPath("powershell.exe"); err != nil {
		t.Skipf("no powershell.exe on PATH: %v", err)
	}
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
	if strings.Contains(err.Error(), "exit status 3") {
		t.Skipf("WMI refused to create a process here: %v", err)
	}
	if !strings.Contains(err.Error(), "cannot open the log") {
		t.Fatalf("the error does not name the cause: %v", err)
	}
	t.Logf("named the cause after %s", took)
	if took > 4*time.Second {
		t.Fatalf("took %s to notice cmd was gone", took)
	}
}
