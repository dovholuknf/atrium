//go:build windows

package ptyhost

import (
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// tree is the job object one runner lives in, so a kill ends what the runner started too.
type tree struct {
	mu  sync.Mutex
	job windows.Handle
}

// newTree puts pid in a job that ends its processes when the handle closes. The job belongs to the runner alone: the
// room exiting never closes it. A failure leaves a tree that only kills the runner.
func newTree(pid int) *tree {
	t := &tree{}
	job, err := windows.CreateJobObject(nil, nil)
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
	for pid := range parents {
		if pid == uint32(root) || !descendsFrom(parents, pid, uint32(root)) {
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

func descendsFrom(parents map[uint32]uint32, pid, root uint32) bool {
	for range len(parents) {
		pp, ok := parents[pid]
		if !ok || pp == 0 {
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
