//go:build windows

package detach

import (
	"strings"
	"testing"
)

func TestOutsideJobCommandLine(t *testing.T) {
	got := outsideJobCommandLine(`C:\a b\atrium.ptyhost.exe`, []string{"ptyhost", "--state-dir", `C:\s d`}, `C:\s d\ptyhost.log`)
	want := `cmd.exe /c ""C:\a b\atrium.ptyhost.exe" "ptyhost" "--state-dir" "C:\s d" >> "C:\s d\ptyhost.log" 2>&1"`
	if got != want {
		t.Fatalf("got  %s\nwant %s", got, want)
	}
	if got := outsideJobCommandLine("x.exe", nil, ""); got != `"x.exe"` {
		t.Fatalf("no log: %s", got)
	}
}

func TestOutsideJobScriptSkipsTheConsoleHost(t *testing.T) {
	s := outsideJobScript()
	if !strings.Contains(s, `$_.Name -ne 'conhost.exe'`) {
		t.Fatalf("the child filter does not exclude conhost.exe: %s", s)
	}
	if !strings.Contains(s, "$p = $r.ProcessId") {
		t.Fatalf("cmd's pid is no longer the fallback: %s", s)
	}
}

func TestOutsideJobScriptHidesTheWindowAndStopsWhenCmdIsGone(t *testing.T) {
	s := outsideJobScript()
	for _, want := range []string{
		"Win32_ProcessStartup", "ShowWindow=[uint16]0", "ProcessStartupInformation=$si",
		"[Diagnostics.Stopwatch]::StartNew()", "-lt 5000",
		`-Filter "ProcessId=$($r.ProcessId)")) { exit 4 }`,
	} {
		if !strings.Contains(s, want) {
			t.Errorf("the script lacks %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "$i -lt 50") {
		t.Errorf("the poll is counted in iterations again:\n%s", s)
	}
}
