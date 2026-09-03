//go:build !windows

package cli

import (
	"os"
	"strconv"
	"strings"
)

// See ancestry_windows.go for what this is for. Here the process table is
// /proc, so there is nothing to snapshot: each hop is one small read.

var runnerNames = map[string]bool{"claude": true, "node": true}

const maxHops = 6

func runnerPID() int {
	walk := os.Getpid()
	for i := 0; i < maxHops && walk > 0; i++ {
		name, parent, ok := procStat(walk)
		if !ok {
			return 0
		}
		if i > 0 && runnerNames[name] {
			return walk
		}
		walk = parent
	}
	return 0
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
