//go:build windows

package detach

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
)

// startOutsideJob is the last resort when a job refuses breakaway. A process cannot leave a job that forbids it, but
// one created by another process can: WMI's Win32_Process.Create makes the child under WmiPrvSE, in no job of ours,
// so a room task whose job is killed with the task no longer takes the pty host and every runner with it.
//
// The child gets the system environment and not ours. The pty host needs none of ours: a spawn carries its own env.
func startOutsideJob(exe string, args []string, out *os.File) (*os.Process, error) {
	script := `$r = Invoke-CimMethod -ClassName Win32_Process -MethodName Create -Arguments @{CommandLine=$env:ATRIUM_DETACH_CMD}; ` +
		`if ($r.ReturnValue -ne 0) { exit 3 }; [Console]::Out.Write($r.ProcessId)`
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command", script)
	cmd.Env = append(os.Environ(), "ATRIUM_DETACH_CMD="+outsideJobCommandLine(exe, args, outName(out)))
	got, err := cmd.Output()
	if err != nil {
		return nil, fmt.Errorf("start outside the job through WMI: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(got)))
	if err != nil || pid <= 0 {
		return nil, fmt.Errorf("start outside the job through WMI: no pid in %q", got)
	}
	return os.FindProcess(pid)
}

func outName(out *os.File) string {
	if out == nil {
		return ""
	}
	return out.Name()
}

// outsideJobCommandLine is what WMI runs: cmd, because WMI hands the child no handles and the redirect is how the
// host's output reaches its log.
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
