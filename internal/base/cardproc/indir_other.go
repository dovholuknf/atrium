//go:build !windows

package cardproc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// InDir is every process, bar this one, whose working directory is dir or under it, each with its start time. A
// process that left its parent for init keeps its cwd, so this finds what Tree cannot.
func InDir(dir string) map[int]string {
	out := map[int]string{}
	if strings.TrimSpace(dir) == "" {
		return out
	}
	if real, err := filepath.EvalSymlinks(dir); err == nil {
		dir = real
	}
	dir = filepath.Clean(dir)
	for pid, cwd := range cwds() {
		if pid == os.Getpid() || (cwd != dir && !strings.HasPrefix(cwd, dir+string(filepath.Separator))) {
			continue
		}
		if st, err := StartTime(pid); err == nil {
			out[pid] = st
		}
	}
	return out
}

// cwds is the working directory of each process that will say. Linux reads /proc, anything else asks lsof.
func cwds() map[int]string {
	out := map[int]string{}
	if ents, err := os.ReadDir("/proc"); err == nil {
		for _, e := range ents {
			pid, perr := strconv.Atoi(e.Name())
			if perr != nil {
				continue
			}
			if cwd, err := os.Readlink("/proc/" + e.Name() + "/cwd"); err == nil {
				out[pid] = cwd
			}
		}
		return out
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	raw, err := exec.CommandContext(ctx, "lsof", "-a", "-d", "cwd", "-Fpn").Output()
	if err != nil && len(raw) == 0 {
		return out
	}
	pid := 0
	for _, l := range strings.Split(string(raw), "\n") {
		switch {
		case strings.HasPrefix(l, "p"):
			pid, _ = strconv.Atoi(l[1:])
		case strings.HasPrefix(l, "n") && pid > 0:
			out[pid] = l[1:]
		}
	}
	return out
}

// StopInDir ends every process InDir finds, SIGKILL, and returns them for WaitGone.
func StopInDir(dir string) map[int]string {
	found := InDir(dir)
	for pid := range found {
		_ = syscall.Kill(pid, syscall.SIGKILL)
	}
	return found
}
