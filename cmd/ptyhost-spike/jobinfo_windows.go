//go:build windows

package main

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobReport says which job object this process is in and what it allows. Handle 0 asks about the caller's own
// job. The two flags that matter to the design: BREAKAWAY_OK lets CREATE_BREAKAWAY_FROM_JOB take a child out, and
// KILL_ON_JOB_CLOSE is what ends every member when the task is ended.
func jobReport() string {
	var in int32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	cur, _ := windows.GetCurrentProcess()
	if r, _, err := proc.Call(uintptr(cur), 0, uintptr(unsafe.Pointer(&in))); r == 0 {
		return fmt.Sprintf("IsProcessInJob: %v", err)
	}
	if in == 0 {
		return "not in a job"
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	var n uint32
	err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &n)
	if err != nil {
		return fmt.Sprintf("in a job, QueryInformationJobObject: %v", err)
	}
	f := info.BasicLimitInformation.LimitFlags
	return fmt.Sprintf("in a job, LimitFlags=%#x BREAKAWAY_OK=%v SILENT_BREAKAWAY_OK=%v KILL_ON_JOB_CLOSE=%v",
		f, f&0x800 != 0, f&0x1000 != 0, f&0x2000 != 0)
}
