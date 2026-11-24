package capability

import (
	"bufio"
	"context"
	"os"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

func cpuModel(context.Context) string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	for s.Scan() {
		k, v, ok := strings.Cut(s.Text(), ":")
		if ok && (strings.TrimSpace(k) == "model name" || strings.TrimSpace(k) == "Model") {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func memory(context.Context) (total, free uint64) {
	b, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	for _, l := range strings.Split(string(b), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		n, err := strconv.ParseUint(f[1], 10, 64)
		if err != nil {
			continue
		}
		switch f[0] {
		case "MemTotal:":
			total = n << 10
		case "MemAvailable:":
			free = n << 10
		}
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

func osGPUs(ctx context.Context, _ Hardware) []GPU {
	return parseROCm(probeOut(ctx, "rocm-smi", "--showproductname", "--showmeminfo", "vram", "--json"))
}
