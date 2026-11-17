package roomstats

import (
	"strconv"
	"strings"
	"time"
)

// HostProc is one process on this host as the operating system gives it without admin. CPU and IO are cumulative and
// mean something only as a difference between two readings. A zero Started or an empty Owner is not known.
type HostProc struct {
	PID     int
	PPID    int
	Name    string
	CPU     time.Duration
	RSS     uint64
	IO      uint64
	HasIO   bool
	Started time.Time
	Owner   string
}

// What a host says about one process's TCP connections. A listening socket is not a connection.
const (
	ConnUnknown = iota
	ConnNone
	ConnSome
)

// SSHDSession is an sshd that serves a connection: sshd-session, or an sshd whose parent is an sshd. The listener's
// parent is the service manager.
func SSHDSession(p HostProc, byPID map[int]HostProc) bool {
	switch exeBase(p.Name) {
	case "sshd-session":
		return true
	case "sshd":
		parent, ok := byPID[p.PPID]
		return ok && p.PPID != p.PID && (exeBase(parent.Name) == "sshd" || exeBase(parent.Name) == "sshd-session")
	}
	return false
}

func exeBase(name string) string {
	name = strings.ToLower(strings.ReplaceAll(name, "\\", "/"))
	if i := strings.LastIndex(name, "/"); i >= 0 {
		name = name[i+1:]
	}
	return strings.TrimSuffix(name, ".exe")
}

// psDuration reads ps's [[dd-]hh:]mm:ss[.cc], which is both etime and cputime.
func psDuration(s string) (time.Duration, bool) {
	var days int
	if d, rest, ok := strings.Cut(s, "-"); ok {
		n, err := strconv.Atoi(d)
		if err != nil {
			return 0, false
		}
		days, s = n, rest
	}
	parts := strings.Split(s, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var secs float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || v < 0 {
			return 0, false
		}
		if i < len(parts)-1 {
			secs = (secs + v) * 60
		} else {
			secs += v
		}
	}
	return time.Duration((float64(days)*86400 + secs) * float64(time.Second)), true
}
