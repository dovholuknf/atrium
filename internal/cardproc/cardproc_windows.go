//go:build windows

package cardproc

import (
	"context"
	"errors"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"github.com/dovholuknf/atrium/internal/nowindow"
)

// StartTime is when pid started, as text that only compares equal for the same process.
func StartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrGone
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return "", ErrGone
		}
		return "", err
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != 259 { // 259 is STILL_ACTIVE
		return "", ErrGone
	}
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return "", err
	}
	return strconv.FormatInt(c.Nanoseconds(), 10), nil
}

// OwnGroup starts cmd in a process group of its own, so a Ctrl+C to atrium's console does not reach it. StopTree
// walks the tree by parent, so nothing more is needed for that.
func OwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// StopTree ends pid and every process it started, through taskkill /T, which walks the tree by parent.
func StopTree(pid int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	nowindow.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, serr := StartTime(pid); errors.Is(serr, ErrGone) {
			return nil
		}
		return errors.New("taskkill: " + string(out))
	}
	return nil
}

// Excluded is the TCP port ranges Windows has reserved (Hyper-V, WSL). A bind there fails with WSAEACCES, and a
// loopback probe can pass on one before it does.
func Excluded() []Range {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netsh", "int", "ipv4", "show", "excludedportrange", "protocol=tcp")
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseExcluded(string(out))
}
