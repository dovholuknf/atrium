//go:build !windows

package cardproc

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// StartTime is when pid started, as text that only compares equal for the same process. Linux reads
// /proc/<pid>/stat, anything else asks ps.
func StartTime(pid int) (string, error) {
	if pid <= 0 {
		return "", ErrGone
	}
	if raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat"); err == nil {
		// The command name is in parentheses and may hold spaces, so the fields are counted after the last ')'.
		s := string(raw)
		if i := strings.LastIndexByte(s, ')'); i >= 0 {
			f := strings.Fields(s[i+1:])
			if len(f) > 19 {
				return f[19], nil
			}
		}
		return "", errors.New("/proc stat does not read")
	} else if errors.Is(err, os.ErrNotExist) {
		if _, serr := os.Stat("/proc/self"); serr == nil {
			return "", ErrGone
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-o", "lstart=", "-p", strconv.Itoa(pid)).Output()
	if err != nil || strings.TrimSpace(string(out)) == "" {
		return "", ErrGone
	}
	return strings.TrimSpace(string(out)), nil
}

// StopTree ends pid and its process group when it leads one: SIGTERM, a short wait, then SIGKILL.
func StopTree(pid int) error {
	target := pid
	if pg, err := syscall.Getpgid(pid); err == nil && pg == pid {
		target = -pid
	}
	if err := syscall.Kill(target, syscall.SIGTERM); err != nil {
		if errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	for i := 0; i < 30; i++ {
		if _, err := StartTime(pid); errors.Is(err, ErrGone) {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	if err := syscall.Kill(target, syscall.SIGKILL); err != nil && !errors.Is(err, syscall.ESRCH) {
		return err
	}
	return nil
}

// OwnGroup makes cmd lead a process group of its own, so StopTree ends what it starts too.
func OwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

// Excluded is the reserved port ranges, which only Windows has.
func Excluded() []Range { return nil }
