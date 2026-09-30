//go:build darwin

package cli

import (
	"os"
	"strings"

	"golang.org/x/sys/unix"
)

// See ancestry_windows.go for what this is for. macOS has no /proc, so each hop
// is one kern.proc.pid sysctl, which answers the name and the parent together.

func runnerPID() int {
	walk := os.Getpid()
	for i := 0; i < maxHops && walk > 0; i++ {
		name, parent, ok := procInfo(walk)
		if !ok {
			return 0
		}
		if i > 0 && (runnerNames[name] || runnerNames[argv0Base(walk)]) {
			return walk
		}
		walk = parent
	}
	return 0
}

// argv0Base is the lowercased basename of a process's argv[0], or "" when it
// cannot be read.
//
// The name from procInfo is not enough on its own: the native claude install
// is a file named for its version, so P_comm says "2.1.285" while argv[0] says
// "claude". Only asked when the name did not match, so node and the shells in
// between cost nothing extra.
func argv0Base(pid int) string {
	raw, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(raw) < 4 {
		return ""
	}
	// argc (int32), then the executable path, then NUL padding, then argv[0].
	rest := raw[4:]
	i := 0
	for i < len(rest) && rest[i] != 0 {
		i++
	}
	for i < len(rest) && rest[i] == 0 {
		i++
	}
	j := i
	for j < len(rest) && rest[j] != 0 {
		j++
	}
	arg := string(rest[i:j])
	if k := strings.LastIndexByte(arg, '/'); k >= 0 {
		arg = arg[k+1:]
	}
	return strings.ToLower(arg)
}

// procInfo reads a process's lowercased name and parent from the kernel.
//
// P_comm is NUL terminated and the kernel truncates it to 16 bytes, which is
// long enough for every name in runnerNames.
func procInfo(pid int) (name string, parent int, ok bool) {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil {
		return "", 0, false
	}
	// A pid that does not exist can come back as an empty record, not an error.
	if int(kp.Proc.P_pid) != pid {
		return "", 0, false
	}
	comm := make([]byte, 0, len(kp.Proc.P_comm))
	for _, c := range kp.Proc.P_comm {
		if c == 0 {
			break
		}
		comm = append(comm, byte(c))
	}
	return strings.ToLower(string(comm)), int(kp.Eproc.Ppid), true
}
