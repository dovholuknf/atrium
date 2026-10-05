//go:build darwin

package roomstats

import (
	"encoding/binary"
	"runtime"

	"golang.org/x/sys/unix"
)

// loadIdle estimates idle CPU from vm.loadavg, which is struct loadavg { fixpt_t ldavg[3]; long fscale }: three
// uint32, four bytes of padding, an int64 scale. The one minute figure over the CPU count, clamped to 0..100.
func loadIdle() (float64, bool) {
	b, err := unix.SysctlRaw("vm.loadavg")
	if err != nil || len(b) < 24 {
		return 0, false
	}
	scale := binary.LittleEndian.Uint64(b[16:24])
	if scale == 0 {
		return 0, false
	}
	load := float64(binary.LittleEndian.Uint32(b[0:4])) / float64(scale)
	idle := 100 * (1 - load/float64(runtime.NumCPU()))
	if idle < 0 {
		idle = 0
	}
	return round1(idle), true
}
