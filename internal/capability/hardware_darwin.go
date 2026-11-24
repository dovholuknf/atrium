package capability

import (
	"context"
	"regexp"
	"strconv"

	"golang.org/x/sys/unix"
)

func cpuModel(context.Context) string {
	s, _ := unix.Sysctl("machdep.cpu.brand_string")
	return s
}

var vmPage = regexp.MustCompile(`page size of (\d+) bytes`)
var vmCount = regexp.MustCompile(`Pages (free|inactive|speculative):\s+(\d+)`)

func memory(ctx context.Context) (total, free uint64) {
	total, _ = unix.SysctlUint64("hw.memsize")
	text := probeOut(ctx, "vm_stat")
	m := vmPage.FindStringSubmatch(text)
	if m == nil {
		return total, 0
	}
	page, _ := strconv.ParseUint(m[1], 10, 64)
	for _, c := range vmCount.FindAllStringSubmatch(text, -1) {
		n, _ := strconv.ParseUint(c[2], 10, 64)
		free += n * page
	}
	return total, free
}

func diskFree(path string) (uint64, bool) {
	var st unix.Statfs_t
	if unix.Statfs(path, &st) != nil {
		return 0, false
	}
	return uint64(st.Bavail) * uint64(st.Bsize), true
}

func osGPUs(ctx context.Context, h Hardware) []GPU {
	return parseProfiler(probeOut(ctx, "system_profiler", "SPDisplaysDataType", "-json"), h.RAM)
}
