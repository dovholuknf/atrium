//go:build linux

package roomstats

import (
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// clkTck is USER_HZ, 100 on every Linux atrium runs on. Reading it needs cgo.
const clkTck = 100

// HostProcs reads /proc. Another account's IO counters need root, so those processes have HasIO false.
func HostProcs() ([]HostProc, error) {
	ents, err := os.ReadDir("/proc")
	if err != nil {
		return nil, err
	}
	boot := bootTime()
	names := map[uint32]string{}
	var out []HostProc
	for _, e := range ents {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		b, err := os.ReadFile("/proc/" + e.Name() + "/stat")
		if err != nil {
			continue
		}
		p, ok := parseProcPIDStat(string(b), boot)
		if !ok {
			continue
		}
		p.PID = pid
		if fi, err := os.Stat("/proc/" + e.Name()); err == nil {
			if st, ok := fi.Sys().(*syscall.Stat_t); ok {
				p.Owner = userName(st.Uid, names)
			}
		}
		if b, err := os.ReadFile("/proc/" + e.Name() + "/io"); err == nil {
			p.IO, p.HasIO = parseProcIO(string(b))
		}
		out = append(out, p)
	}
	return out, nil
}

// parseProcPIDStat reads /proc/<pid>/stat. The name sits in parentheses and may hold spaces, so fields count from the
// last ')'.
func parseProcPIDStat(text string, boot time.Time) (HostProc, bool) {
	open, end := strings.IndexByte(text, '('), strings.LastIndexByte(text, ')')
	if open < 0 || end < open {
		return HostProc{}, false
	}
	f := strings.Fields(text[end+1:])
	if len(f) < 22 {
		return HostProc{}, false
	}
	// 62 bits and 31 bits: the bounds keep the conversions to int64 and int below from wrapping.
	num := func(i int) uint64 { n, _ := strconv.ParseUint(f[i], 10, 62); return n }
	ppid, _ := strconv.ParseUint(f[1], 10, 31)
	p := HostProc{Name: text[open+1 : end], PPID: int(ppid),
		CPU: time.Duration(num(11)+num(12)) * time.Second / clkTck, RSS: num(21) * uint64(os.Getpagesize())}
	if !boot.IsZero() {
		p.Started = boot.Add(time.Duration(num(19)) * time.Second / clkTck)
	}
	return p, true
}

func parseProcIO(text string) (uint64, bool) {
	var n uint64
	var seen bool
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, ":")
		if !ok || (k != "read_bytes" && k != "write_bytes") {
			continue
		}
		x, err := strconv.ParseUint(strings.TrimSpace(v), 10, 64)
		if err != nil {
			return 0, false
		}
		n, seen = n+x, true
	}
	return n, seen
}

func bootTime() time.Time {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return time.Time{}
	}
	for _, line := range strings.Split(string(b), "\n") {
		if v, ok := strings.CutPrefix(line, "btime "); ok {
			if s, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
				return time.Unix(s, 0)
			}
		}
	}
	return time.Time{}
}

func userName(uid uint32, names map[uint32]string) string {
	if n, ok := names[uid]; ok {
		return n
	}
	n := strconv.FormatUint(uint64(uid), 10)
	if u, err := user.LookupId(n); err == nil {
		n = u.Username
	}
	names[uid] = n
	return n
}

// OwnerOf is filled by HostProcs on Linux.
func OwnerOf(int) string { return "" }

// HostConns matches each process's socket descriptors to /proc/net/tcp. Another account's descriptors need root, so
// its answer is ConnUnknown.
func HostConns(pids []int) map[int]int {
	live := map[string]bool{}
	for _, f := range []string{"/proc/net/tcp", "/proc/net/tcp6"} {
		if b, err := os.ReadFile(f); err == nil {
			for ino := range tcpInodes(string(b)) {
				live[ino] = true
			}
		}
	}
	out := map[int]int{}
	for _, pid := range pids {
		dir := "/proc/" + strconv.Itoa(pid) + "/fd"
		fds, err := os.ReadDir(dir)
		if err != nil {
			out[pid] = ConnUnknown
			continue
		}
		out[pid] = ConnNone
		for _, fd := range fds {
			l, err := os.Readlink(filepath.Join(dir, fd.Name()))
			if err == nil && strings.HasPrefix(l, "socket:[") && live[strings.TrimSuffix(l[8:], "]")] {
				out[pid] = ConnSome
				break
			}
		}
	}
	return out
}

// tcpInodes is the inode of every row of /proc/net/tcp that is not listening (0A).
func tcpInodes(text string) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(text, "\n") {
		f := strings.Fields(line)
		if len(f) < 10 || f[3] == "0A" || f[0] == "sl" {
			continue
		}
		out[f[9]] = true
	}
	return out
}
