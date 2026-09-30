//go:build windows

package roomstats

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	kernel32             = windows.NewLazySystemDLL("kernel32.dll")
	getSystemTimes       = kernel32.NewProc("GetSystemTimes")
	globalMemoryStatusEx = kernel32.NewProc("GlobalMemoryStatusEx")
)

type memoryStatusEx struct {
	Length               uint32
	MemoryLoad           uint32
	TotalPhys            uint64
	AvailPhys            uint64
	TotalPageFile        uint64
	AvailPageFile        uint64
	TotalVirtual         uint64
	AvailVirtual         uint64
	AvailExtendedVirtual uint64
}

// ReadMachine is GetSystemTimes and GlobalMemoryStatusEx, never CIM or WMI.
// Kernel time includes idle, so total is kernel plus user.
func ReadMachine() (MachineReading, error) {
	var idle, kernel, user windows.Filetime
	if r, _, err := getSystemTimes.Call(uintptr(unsafe.Pointer(&idle)), uintptr(unsafe.Pointer(&kernel)), uintptr(unsafe.Pointer(&user))); r == 0 {
		return MachineReading{}, err
	}
	ticks := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
	ms := memoryStatusEx{}
	ms.Length = uint32(unsafe.Sizeof(ms))
	if r, _, err := globalMemoryStatusEx.Call(uintptr(unsafe.Pointer(&ms))); r == 0 {
		return MachineReading{}, err
	}
	return MachineReading{
		HasCPU: true, CPUIdle: ticks(idle), CPUTotal: ticks(kernel) + ticks(user),
		MemUsed: ms.TotalPhys - ms.AvailPhys, MemTotal: ms.TotalPhys,
	}, nil
}
