//go:build !windows && !linux

package ptyhost

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// intoScope has nothing to move pid into here. The process group and ATRIUM_TASK_ID say whose it is.
func intoScope(int, string, string) string { return "" }

// treeMem is the resident memory of pid's process group, from ps.
func treeMem(pid int, _ string) int64 {
	cmd := exec.Command("ps", "-A", "-o", "pgid=,rss=")
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return 0
	}
	var kb int64
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) != 2 || f[0] != strconv.Itoa(pid) {
			continue
		}
		n, _ := strconv.ParseInt(f[1], 10, 64)
		kb += n
	}
	return kb * 1024
}
