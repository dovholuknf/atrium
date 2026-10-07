//go:build windows

package gitsync

import (
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/dovholuknf/atrium/internal/nowindow"
)

// procTree is a job object holding one git child and everything it starts.
//
// WHY A JOB. `git fetch` over http runs `git-remote-http`, which Windows does not end when
// its parent is killed. It kept the bare repository open after Stop, so the directory could
// not be removed, and a stopped runner still had a process talking to a room. Every process
// started inside a job belongs to it, and closing a job made with KILL_ON_JOB_CLOSE ends
// all of them.
//
// A process the child starts in the moment between Start and the assignment below escapes
// the job. git does its startup work first, so in practice nothing does, but this is not a
// guarantee.
type procTree struct{ job windows.Handle }

// startTree puts a started command into a new job. A failure answers nil, and the caller
// falls back to killing the child alone, which is what happened before.
func startTree(cmd *exec.Cmd) *procTree {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return nil
	}
	proc, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false,
		uint32(cmd.Process.Pid))
	if err != nil {
		windows.CloseHandle(job)
		return nil
	}
	defer windows.CloseHandle(proc)
	if err := windows.AssignProcessToJobObject(job, proc); err != nil {
		windows.CloseHandle(job)
		return nil
	}
	return &procTree{job: job}
}

// kill ends every process in the tree.
func (t *procTree) kill() error { return windows.TerminateJobObject(t.job, 1) }

// close ends whatever is left and releases the job.
func (t *procTree) close() { windows.CloseHandle(t.job) }

// prepareTree starts the child without a console window. The hub and the rooms have no
// console, so without this every git child opens one on the operator's desktop. The job
// is made after Start.
func prepareTree(cmd *exec.Cmd) { nowindow.Hide(cmd) }
