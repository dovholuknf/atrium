//go:build windows

package main

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// jobtest stands in for a Task Scheduler task, which this account cannot register on sg4 (see the write-up). It puts
// a `parent` in a job object with KILL_ON_JOB_CLOSE, which is what ending a task does to everything the task started,
// then closes the job and leaves the caller to see whether the host and its runner survived.
//
//	ptyhost-spike jobtest -mode none|ok|silent -after 8s -- parent args...
//
// mode says what the job allows: none refuses breakaway, ok allows it when CREATE_BREAKAWAY_FROM_JOB is asked for,
// silent lets a child leave without asking.
func init() {
	extraModes["jobtest"] = func(args []string) {
		mode, after := "none", 8*time.Second
		i := 0
		for ; i < len(args); i++ {
			switch args[i] {
			case "-mode":
				mode = args[i+1]
				i++
			case "-after":
				after, _ = time.ParseDuration(args[i+1])
				i++
			case "--":
				i++
				goto done
			}
		}
	done:
		job, err := windows.CreateJobObject(nil, nil)
		if err != nil {
			fatal("CreateJobObject: %v", err)
		}
		var info windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
		f := uint32(0x2000) // KILL_ON_JOB_CLOSE
		switch mode {
		case "ok":
			f |= 0x800
		case "silent":
			f |= 0x1000
		}
		info.BasicLimitInformation.LimitFlags = f
		if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
			uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
			fatal("SetInformationJobObject: %v", err)
		}
		exe, _ := os.Executable()
		cmd := exec.Command(exe, append([]string{"parent"}, args[i:]...)...)
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		// CREATE_SUSPENDED so the parent is in the job before it runs a single instruction
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x4 | syscall.CREATE_NEW_PROCESS_GROUP}
		if err := cmd.Start(); err != nil {
			fatal("start parent: %v", err)
		}
		h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
		if err != nil {
			fatal("OpenProcess: %v", err)
		}
		if err := windows.AssignProcessToJobObject(job, h); err != nil {
			fatal("AssignProcessToJobObject: %v", err)
		}
		resumeMain(cmd.Process.Pid)
		logf("jobtest: parent %d is in a KILL_ON_JOB_CLOSE job, breakaway mode %s. ending it in %s", cmd.Process.Pid, mode, after)
		time.Sleep(after)
		logf("jobtest: CLOSING THE JOB (what ending the task does)")
		windows.TerminateJobObject(job, 1)
		windows.CloseHandle(job)
		logf("jobtest: job closed")
	}
}

// resumeMain resumes the one thread of a process started CREATE_SUSPENDED.
func resumeMain(pid int) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPTHREAD, 0)
	if err != nil {
		fatal("snapshot: %v", err)
	}
	defer windows.CloseHandle(snap)
	var te windows.ThreadEntry32
	te.Size = uint32(unsafe.Sizeof(te))
	for err = windows.Thread32First(snap, &te); err == nil; err = windows.Thread32Next(snap, &te) {
		if te.OwnerProcessID == uint32(pid) {
			th, err := windows.OpenThread(windows.THREAD_SUSPEND_RESUME, false, te.ThreadID)
			if err != nil {
				fatal("OpenThread: %v", err)
			}
			_, _ = windows.ResumeThread(th)
			windows.CloseHandle(th)
			return
		}
	}
	fatal("no thread for %d", pid)
}

var _ = fmt.Sprint
