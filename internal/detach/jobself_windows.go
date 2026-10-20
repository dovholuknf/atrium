//go:build windows

package detach

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// InJob says whether the process pid is inside any job object. A job handle of 0 asks about every job, which is the
// question: a process that left one job through breakaway can still sit in the job above it.
func InJob(pid int) (bool, error) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false, fmt.Errorf("open process %d: %w", pid, err)
	}
	defer windows.CloseHandle(h)
	return inJobHandle(h)
}

func selfInJob() (bool, error) {
	cur, _ := windows.GetCurrentProcess()
	return inJobHandle(cur)
}

func inJobHandle(h windows.Handle) (bool, error) {
	var in int32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	if r, _, err := proc.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&in))); r == 0 {
		return false, fmt.Errorf("IsProcessInJob: %w", err)
	}
	return in != 0, nil
}
