//go:build !windows

package roomstats

import (
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
)

// ListTop is the n busiest processes by ps, with each one's cwd.
func ListTop(n int) ([]Proc, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,etime=,pcpu=,command=").Output()
	if err != nil {
		return nil, err
	}
	ps := TopByCPU(parsePS(string(out)), n)
	for i := range ps {
		ps[i].Cwd = procCwd(ps[i].PID)
	}
	return ps, nil
}

// ProcByPID is one process with its cwd. ok is false when it is gone.
func ProcByPID(pid int) (Proc, bool, error) {
	out, err := exec.Command("ps", "-o", "pid=,ppid=,etime=,pcpu=,command=", "-p", strconv.Itoa(pid)).Output()
	ps := parsePS(string(out))
	if len(ps) == 0 {
		if _, ok := err.(*exec.ExitError); ok || err == nil {
			return Proc{}, false, nil
		}
		return Proc{}, false, err
	}
	p := TopByCPU(ps, 1)[0]
	p.Cwd = procCwd(pid)
	return p, true, nil
}

// procCwd is empty when it cannot be read, which classifies by command alone.
func procCwd(pid int) string {
	if runtime.GOOS == "linux" {
		p, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd")
		return p
	}
	out, err := exec.Command("lsof", "-a", "-d", "cwd", "-p", strconv.Itoa(pid), "-Fn").Output()
	if err != nil {
		return ""
	}
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n") {
			return l[1:]
		}
	}
	return ""
}
