//go:build !windows

package roomstats

import (
	"fmt"
	"os"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// ProcessTimes is this process's CPU time and resident bytes. /proc on Linux.
// Elsewhere the peak from getrusage, which is what the kernel offers without
// exec'ing ps.
func ProcessTimes() (time.Duration, uint64, error) {
	var ru syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &ru); err != nil {
		return 0, 0, err
	}
	cpu := time.Duration(ru.Utime.Nano() + ru.Stime.Nano())
	if runtime.GOOS == "linux" {
		b, err := os.ReadFile("/proc/self/statm")
		if err != nil {
			return 0, 0, err
		}
		f := strings.Fields(string(b))
		if len(f) < 2 {
			return 0, 0, fmt.Errorf("short statm")
		}
		pages, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			return 0, 0, err
		}
		return cpu, pages * uint64(os.Getpagesize()), nil
	}
	return cpu, uint64(ru.Maxrss), nil
}

// DiskOf is free and total bytes of the volume holding path.
func DiskOf(path string) (string, uint64, uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return path, 0, 0, err
	}
	bs := uint64(st.Bsize)
	return path, uint64(st.Bavail) * bs, uint64(st.Blocks) * bs, nil
}
