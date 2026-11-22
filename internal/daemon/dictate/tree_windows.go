//go:build windows

package dictate

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procTree is a job made with KILL_ON_JOB_CLOSE, so the server and its GPU memory go when the room does, even on a
// crash. The handle is held for the server's life.
type procTree struct{ job windows.Handle }

func startTree(cmd *exec.Cmd) *procTree {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false,
		uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		_ = windows.CloseHandle(job)
		return nil
	}
	return &procTree{job: job}
}

func (t *procTree) kill() error { return windows.TerminateJobObject(t.job, 1) }

func (t *procTree) close() { _ = windows.CloseHandle(t.job) }
