//go:build !windows && !linux

package roomstats

import (
	"os/exec"
	"strconv"
	"strings"
	"time"
)

// HostProcs reads ps. macOS gives no process's IO counters without root.
func HostProcs() ([]HostProc, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,user=,etime=,time=,rss=,comm=").Output()
	if err != nil {
		return nil, err
	}
	return parseHostPS(string(out), time.Now()), nil
}

func parseHostPS(text string, now time.Time) []HostProc {
	var out []HostProc
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 7 {
			continue
		}
		pid, err1 := strconv.Atoi(f[0])
		ppid, err2 := strconv.Atoi(f[1])
		age, ok1 := psDuration(f[3])
		cpu, ok2 := psDuration(f[4])
		rss, err3 := strconv.ParseUint(f[5], 10, 64)
		if err1 != nil || err2 != nil || err3 != nil || !ok1 || !ok2 {
			continue
		}
		out = append(out, HostProc{PID: pid, PPID: ppid, Owner: f[2], Started: now.Add(-age), CPU: cpu, RSS: rss << 10,
			Name: strings.Join(f[6:], " ")})
	}
	return out
}

// OwnerOf is filled by HostProcs here.
func OwnerOf(int) string { return "" }

// HostConns cannot say here: lsof lists only this account's sockets without root, and ps does not say whose they are.
func HostConns(pids []int) map[int]int {
	out := map[int]int{}
	for _, pid := range pids {
		out[pid] = ConnUnknown
	}
	return out
}
