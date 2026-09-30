//go:build windows

package ptyhost

import (
	"os"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// raise is internal/daemon/priority_windows.go's ptyRaise, for the ConPTYs the HOST opens now. The reasoning is
// there and is not repeated: a keystroke crosses the runner and its console host, and both at Normal overslept by
// 15 to 149ms under load. Above normal, and the runner's children are left alone on purpose. The daemon's
// version reads a setting and belongs to the daemon, so this is the same steps without the store: on unless
// ATRIUM_PRIORITY=normal, the switch the CLI's own raise honours.
//
// Never fails a spawn.
type raise struct {
	on     bool
	before map[uint32]bool
}

// beginRaise is called BEFORE pty.New, with the host's spawns serialised, so the console hosts that appear after
// it are this pty's.
func beginRaise() *raise {
	r := &raise{on: !strings.EqualFold(strings.TrimSpace(os.Getenv("ATRIUM_PRIORITY")), "normal")}
	if r.on {
		r.before = consoleHosts()
	}
	return r
}

// apply raises the runner and the new console host. It returns the first error, for the caller to log.
func (r *raise) apply(pid int) error {
	if r == nil || !r.on {
		return nil
	}
	targets := []uint32{uint32(pid)}
	for host := range consoleHosts() {
		if !r.before[host] {
			targets = append(targets, host)
		}
	}
	var first error
	for _, t := range targets {
		if err := raiseProcess(t); err != nil && first == nil {
			first = err
		}
	}
	return first
}

func raiseProcess(pid uint32) error {
	h, err := windows.OpenProcess(windows.PROCESS_SET_INFORMATION, false, pid)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(h)
	return windows.SetPriorityClass(h, windows.ABOVE_NORMAL_PRIORITY_CLASS)
}

// consoleHosts is every conhost.exe or OpenConsole.exe that is a child of this process.
func consoleHosts() map[uint32]bool {
	out := map[uint32]bool{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	me := uint32(os.Getpid())
	for err := windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		if e.ParentProcessID != me {
			continue
		}
		name := strings.ToLower(windows.UTF16ToString(e.ExeFile[:]))
		if name == "conhost.exe" || name == "openconsole.exe" {
			out[e.ProcessID] = true
		}
	}
	return out
}
