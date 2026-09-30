//go:build !windows && !darwin

package cli

import (
	"os"
	"strconv"
	"strings"
)

// See ancestry_windows.go for what this is for. Here the process table is
// /proc, so there is nothing to snapshot: each hop is one small read.

func runnerPID() int {
	walk := os.Getpid()
	for i := 0; i < maxHops && walk > 0; i++ {
		name, parent, ok := procStat(walk)
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
// The name in /proc/<pid>/stat is not enough on its own: the native claude
// install is a file named for its version, so the kernel says "2.1.285" while
// argv[0] says "claude". Only asked when the name did not match, so node and
// the shells in between cost nothing extra. The darwin file does the same.
func argv0Base(pid int) string {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/cmdline")
	if err != nil {
		return ""
	}
	if i := strings.IndexByte(string(raw), 0); i >= 0 {
		raw = raw[:i]
	}
	arg := string(raw)
	if k := strings.LastIndexByte(arg, '/'); k >= 0 {
		arg = arg[k+1:]
	}
	return strings.ToLower(arg)
}

// procStat reads a process's name and parent from /proc/<pid>/stat.
//
// The name is in parentheses and may itself contain them, so the fields after
// it are found from the LAST close paren rather than by splitting the line.
func procStat(pid int) (name string, parent int, ok bool) {
	raw, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/stat")
	if err != nil {
		return "", 0, false
	}
	line := string(raw)
	open := strings.IndexByte(line, '(')
	close := strings.LastIndexByte(line, ')')
	if open < 0 || close < open {
		return "", 0, false
	}
	name = strings.ToLower(line[open+1 : close])

	// After the name come the state and then the parent id.
	rest := strings.Fields(line[close+1:])
	if len(rest) < 2 {
		return "", 0, false
	}
	parent, err = strconv.Atoi(rest[1])
	if err != nil {
		return "", 0, false
	}
	return name, parent, true
}
