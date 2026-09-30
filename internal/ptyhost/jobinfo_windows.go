//go:build windows

package ptyhost

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

// inJob says whether this process is inside a job object, and describes it. Started with CREATE_BREAKAWAY_FROM_JOB
// the host is outside its starter's job wherever the job allows breakaway. A host still inside one is there because
// the job REFUSED breakaway (detach.Start retried without the flag), and ending that job ends the host and every
// runner with it. That is the answer `probe` gives as in_job (design G5).
//
// Handle 0 asks about the caller's own job.
func inJob() (bool, string) {
	var in int32
	proc := windows.NewLazySystemDLL("kernel32.dll").NewProc("IsProcessInJob")
	cur, _ := windows.GetCurrentProcess()
	if r, _, err := proc.Call(uintptr(cur), 0, uintptr(unsafe.Pointer(&in))); r == 0 {
		return false, fmt.Sprintf("IsProcessInJob failed: %v", err)
	}
	if in == 0 {
		return false, "not in a job"
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	var n uint32
	err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &n)
	if err != nil {
		return true, fmt.Sprintf("in a job, QueryInformationJobObject: %v", err)
	}
	f := info.BasicLimitInformation.LimitFlags
	return true, fmt.Sprintf("in a job, LimitFlags=%#x BREAKAWAY_OK=%v SILENT_BREAKAWAY_OK=%v KILL_ON_JOB_CLOSE=%v",
		f, f&0x800 != 0, f&0x1000 != 0, f&0x2000 != 0)
}
