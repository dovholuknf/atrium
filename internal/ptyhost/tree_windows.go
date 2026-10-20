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
	t.job = job
	return t
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
