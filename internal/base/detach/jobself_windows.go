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

const (
	jobLimitActiveProcess = 0x8
	jobLimitProcessMemory = 0x100
	jobLimitJobMemory     = 0x200
	jobLimitBreakawayOK   = 0x800
	jobLimitSilentBreak   = 0x1000
	jobLimitKillOnClose   = 0x2000
)

// JobBinds says whether a limit-flags word describes a job that ends or limits what is in it.
func JobBinds(flags uint32) bool {
	return flags&(jobLimitKillOnClose|jobLimitActiveProcess|jobLimitProcessMemory|jobLimitJobMemory) != 0
}

// SelfJob says whether this process is in a job object, and whether that job kills or limits it. A job with none of
// those flags, such as a terminal's or an agent host's that only allows breakaway, ends nothing.
func SelfJob() (in, binds bool, detail string) {
	cur, _ := windows.GetCurrentProcess()
	in, err := inJobHandle(cur)
	if err != nil {
		return false, false, err.Error()
	}
	if !in {
		return false, false, "not in a job"
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	var n uint32
	if err := windows.QueryInformationJobObject(0, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)), &n); err != nil {
		return true, true, fmt.Sprintf("in a job, QueryInformationJobObject: %v", err)
	}
	f := info.BasicLimitInformation.LimitFlags
	binds = JobBinds(f)
	return true, binds, fmt.Sprintf("in a job, LimitFlags=%#x BREAKAWAY_OK=%v SILENT_BREAKAWAY_OK=%v "+
		"KILL_ON_JOB_CLOSE=%v limits=%v", f, f&jobLimitBreakawayOK != 0, f&jobLimitSilentBreak != 0,
		f&jobLimitKillOnClose != 0, binds)
}
