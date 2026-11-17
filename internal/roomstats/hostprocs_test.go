package roomstats

import (
	"testing"
	"time"
)

func TestPSDuration(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"05:07":       5*time.Minute + 7*time.Second,
		"0:00.50":     500 * time.Millisecond,
		"01:02:03":    time.Hour + 2*time.Minute + 3*time.Second,
		"2-01:00:00":  49 * time.Hour,
		"10-00:00:01": 240*time.Hour + time.Second,
	} {
		if got, ok := psDuration(in); !ok || got != want {
			t.Errorf("psDuration(%q) = %s %v, want %s", in, got, ok, want)
		}
	}
	for _, bad := range []string{"", "12", "a:b", "1-2-3:00"} {
		if _, ok := psDuration(bad); ok {
			t.Errorf("psDuration(%q) should not parse", bad)
		}
	}
}

func TestSSHDSession(t *testing.T) {
	by := map[int]HostProc{
		1:  {PID: 1, Name: "systemd"},
		10: {PID: 10, PPID: 1, Name: "sshd"},
		11: {PID: 11, PPID: 10, Name: "sshd"},
		12: {PID: 12, PPID: 10, Name: `C:\Windows\System32\OpenSSH\sshd-session.exe`},
		13: {PID: 13, PPID: 11, Name: "bash"},
	}
	for pid, want := range map[int]bool{1: false, 10: false, 11: true, 12: true, 13: false} {
		if got := SSHDSession(by[pid], by); got != want {
			t.Errorf("pid %d: SSHDSession = %v, want %v", pid, got, want)
		}
	}
}
