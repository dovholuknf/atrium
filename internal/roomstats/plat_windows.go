//go:build windows

package roomstats

import (
	"path/filepath"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")

type memCounters struct {
	cb                         uint32
	PageFaultCount             uint32
	PeakWorkingSetSize         uintptr
	WorkingSetSize             uintptr
	QuotaPeakPagedPoolUsage    uintptr
	QuotaPagedPoolUsage        uintptr
	QuotaPeakNonPagedPoolUsage uintptr
	QuotaNonPagedPoolUsage     uintptr
	PagefileUsage              uintptr
	PeakPagefileUsage          uintptr
}

// ProcessTimes is this process's CPU time and working set. GetProcessTimes and
// GetProcessMemoryInfo, never CIM or WMI, which answer access denied over ssh.
func ProcessTimes() (time.Duration, uint64, error) {
	h := windows.CurrentProcess()
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return 0, 0, err
	}
	ticks := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	cpu := time.Duration(ticks(k)+ticks(u)) * 100 // FILETIME is 100ns units
	mc := memCounters{}
	mc.cb = uint32(unsafe.Sizeof(mc))
	r, _, err := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.cb))
	if r == 0 {
		return 0, 0, err
	}
	return cpu, uint64(mc.WorkingSetSize), nil
}

// DiskOf is free and total bytes of the volume holding path.
func DiskOf(path string) (string, uint64, uint64, error) {
	p, err := windows.UTF16PtrFromString(filepath.FromSlash(path))
	if err != nil {
		return path, 0, 0, err
	}
	var free, total, totalFree uint64
	if err := windows.GetDiskFreeSpaceEx(p, &free, &total, &totalFree); err != nil {
		return path, 0, 0, err
	}
	return filepath.ToSlash(path), free, total, nil
}
