//go:build windows

package detach

import (
	"errors"
	"fmt"
	"github.com/dovholuknf/atrium/internal/base/nowindow"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// powershellPath is the system copy, so a PATH without powershell.exe does not turn the WMI start off.
func powershellPath() string {
	if root := os.Getenv("SystemRoot"); root != "" {
		p := filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "powershell.exe"
}

// A process WMI creates is WmiPrvSE's child, in no job of ours, which is the only way out of a job that forbids breakaway.
func startOutsideJob(exe string, args []string, log string) (*os.Process, error) {
	cmd := exec.Command(powershellPath(), "-NoProfile", "-NonInteractive", "-Command", outsideJobScript())
	nowindow.Hide(cmd)
	cmd.Env = append(os.Environ(), "ATRIUM_DETACH_CMD="+outsideJobCommandLine(exe, args, log))
	got, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 4 {
			return nil, fmt.Errorf("start outside the job through WMI: cmd.exe ended before it started %s, "+
				"as it does when its redirect cannot open the log %q", filepath.Base(exe), log)
		}
		return nil, fmt.Errorf("start outside the job through WMI: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(got)))
	if err != nil || pid <= 0 {
		return nil, fmt.Errorf("start outside the job through WMI: no pid in %q", got)
	}
	p, err := holdProcess(pid)
	if err != nil {
		return nil, fmt.Errorf("start outside the job through WMI: %s started as pid %d and had ended before it could be held: %w",
			filepath.Base(exe), pid, err)
	}
	return p, nil
}

// A process still being set up answers OpenProcess with access denied, so only that is retried, and only briefly.
func holdProcess(pid int) (*os.Process, error) {
	var err error
	for i := 0; i < 20; i++ {
		var p *os.Process
		if p, err = os.FindProcess(pid); err == nil {
			return p, nil
		}
		if !errors.Is(err, syscall.ERROR_ACCESS_DENIED) {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	return nil, err
}

// cmd.exe, because WMI hands the child no handles and the redirect is the only way its output reaches a log.
func outsideJobCommandLine(exe string, args []string, log string) string {
	parts := []string{`"` + exe + `"`}
	for _, a := range args {
		parts = append(parts, `"`+a+`"`)
	}
	line := strings.Join(parts, " ")
	if log == "" {
		return `cmd.exe /c "` + line + ` >NUL 2>&1"`
	}
	return `cmd.exe /c "` + line + ` >> "` + log + `" 2>&1"`
}

// Answers the child's pid, not cmd's, skipping conhost.exe, and exits 4 when cmd is gone before any child appeared.
func outsideJobScript() string {
	return `$si = New-CimInstance -ClassName Win32_ProcessStartup -ClientOnly -Property @{ShowWindow=[uint16]0}; ` +
		`$r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=$env:ATRIUM_DETACH_CMD; ProcessStartupInformation=$si}; ` +
		`if ($r.ReturnValue -ne 0) { exit 3 }; ` +
		`$p = $r.ProcessId; ` +
		`$sw = [Diagnostics.Stopwatch]::StartNew(); ` +
		`while ($sw.ElapsedMilliseconds -lt 5000) { ` +
		`$k = Get-CimInstance Win32_Process -Filter "ParentProcessId=$($r.ProcessId)" | Where-Object { $_.Name -ne 'conhost.exe' } | Select-Object -First 1; ` +
		`if ($k) { $p = $k.ProcessId; break }; ` +
		`if (-not (Get-CimInstance Win32_Process -Filter "ProcessId=$($r.ProcessId)")) { exit 4 }; ` +
		`[Threading.Thread]::Sleep(100) ` +
		`}; [Console]::Out.Write($p)`
}
