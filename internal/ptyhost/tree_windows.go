//go:build windows

package ptyhost

import (
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	openJobObject        = windows.NewLazySystemDLL("kernel32.dll").NewProc("OpenJobObjectW")
	getProcessMemoryInfo = windows.NewLazySystemDLL("psapi.dll").NewProc("GetProcessMemoryInfo")
)

// tree is the job object one runner lives in, so a kill ends what the runner started too.
type tree struct {
	mu  sync.Mutex
	job windows.Handle
}

// newTree puts pid in a job that ends its processes when the handle closes. The job belongs to the runner alone: the
// room exiting never closes it. A failure leaves a tree that only kills the runner. The job is named after label, so
// Process Explorer's Job tab says whose the runner is.
func newTree(pid int, label, runID string) *tree {
	t := &tree{}
	job, err := windows.CreateJobObject(nil, jobName(label, runID))
	if err != nil {
		return t
	}
	var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return t
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return t
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		_ = windows.CloseHandle(job)
		return t
	}
	adoptDescendants(job, pid)
	t.job = job
	return t
}

// adoptDescendants moves what the runner started before it was in the job into it, since a job only catches children
// created after the assignment.
func adoptDescendants(job windows.Handle, root int) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return
	}
	defer windows.CloseHandle(snap)
	parents := map[uint32]uint32{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		parents[e.ProcessID] = e.ParentProcessID
	}
	created := map[uint32]uint64{}
	for pid := range parents {
		created[pid] = createdAt(pid)
	}
	for pid := range parents {
		if pid == uint32(root) || !descendsFrom(parents, created, pid, uint32(root)) {
			continue
		}
		h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, pid)
		if err != nil {
			continue
		}
		_ = windows.AssignProcessToJobObject(job, h)
		_ = windows.CloseHandle(h)
	}
}

// createdAt is a process's creation time, 0 when it cannot be read.
func createdAt(pid uint32) uint64 {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(h)
	var c, x, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &x, &k, &u); err != nil {
		return 0
	}
	return uint64(c.HighDateTime)<<32 | uint64(c.LowDateTime)
}

// descendsFrom follows parent ids up to root, and refuses a link whose child is older than its parent: pids are reused,
// so that child is an orphan of a dead process that had the same number.
func descendsFrom(parents map[uint32]uint32, created map[uint32]uint64, pid, root uint32) bool {
	for range len(parents) {
		pp, ok := parents[pid]
		if !ok || pp == 0 || created[pid] == 0 || created[pp] == 0 || created[pid] < created[pp] {
			return false
		}
		if pp == root {
			return true
		}
		pid = pp
	}
	return false
}

// kill ends the whole tree and releases the job. It does not wait.
func (t *tree) kill() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job != 0 {
		_ = windows.TerminateJobObject(t.job, 1)
		_ = windows.CloseHandle(t.job)
		t.job = 0
	}
}

// jobName is label as a job name, or nil for none. A name already taken gets the run id after it, since creating a
// job under a taken name opens that job instead.
func jobName(label, runID string) *uint16 {
	if label == "" {
		return nil
	}
	name := strings.ReplaceAll(label, `\`, "/")
	if r := []rune(name); len(r) > 200 {
		name = string(r[:200])
	}
	if jobExists(name) {
		name += " " + runID
	}
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return nil
	}
	return p
}

func jobExists(name string) bool {
	p, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return false
	}
	const jobObjectQuery = 0x0004
	h, _, _ := openJobObject.Call(jobObjectQuery, 0, uintptr(unsafe.Pointer(p)))
	if h == 0 {
		return false
	}
	_ = windows.CloseHandle(windows.Handle(h))
	return true
}

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

// mem is the working set of every process in the job, 0 when it cannot be read.
func (t *tree) mem() int64 {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.job == 0 {
		return 0
	}
	const most = 4096
	var buf struct {
		assigned, listed uint32
		pids             [most]uintptr
	}
	if err := windows.QueryInformationJobObject(t.job, windows.JobObjectBasicProcessIdList,
		uintptr(unsafe.Pointer(&buf)), uint32(unsafe.Sizeof(buf)), nil); err != nil {
		return 0
	}
	var sum int64
	for _, pid := range buf.pids[:min(buf.listed, most)] {
		h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
		if err != nil {
			continue
		}
		mc := memCounters{}
		mc.cb = uint32(unsafe.Sizeof(mc))
		if r, _, _ := getProcessMemoryInfo.Call(uintptr(h), uintptr(unsafe.Pointer(&mc)), uintptr(mc.cb)); r != 0 {
			sum += int64(mc.WorkingSetSize)
		}
		_ = windows.CloseHandle(h)
	}
	return sum
}
