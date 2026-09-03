//go:build windows

package cli

import (
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// Finding the runner this hook is a descendant of.
//
// Atrium wants the runner's own process id, so liveness later is a question for
// the operating system rather than for the agent. A hook runs as a grandchild
// of that process, so the answer is up the tree.
//
// One snapshot of every process, walked in memory. The PowerShell this replaces
// asked WMI per process id, which cost about a second each and up to six and a
// half seconds per session start on this machine. CreateToolhelp32Snapshot is
// the same information for a few milliseconds.

// runnerNames are the process names a claude session runs under. Checked
// without an extension, since that differs by platform and by how it was
// installed.
var runnerNames = map[string]bool{"claude": true, "node": true}

// maxHops bounds the walk. A hook is two or three processes below its runner,
// and a bound means a corrupt or cyclic parent chain cannot spin here.
const maxHops = 6

// runnerPID walks up from this process and returns the first ancestor that
// looks like a runner, or 0 when none is found.
//
// This process is skipped: atrium is not the runner, whatever it is called.
func runnerPID() int {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return 0
	}
	defer windows.CloseHandle(snap)

	type entry struct {
		name   string
		parent uint32
	}
	procs := map[uint32]entry{}

	var e windows.ProcessEntry32
	// Size has to be set before the first call or the API refuses it.
	e.Size = uint32(unsafe.Sizeof(e))
	if err := windows.Process32First(snap, &e); err != nil {
		return 0
	}
	for {
		procs[e.ProcessID] = entry{name: windows.UTF16ToString(e.ExeFile[:]), parent: e.ParentProcessID}
		if err := windows.Process32Next(snap, &e); err != nil {
			break
		}
	}

	walk := uint32(windows.GetCurrentProcessId())
	for i := 0; i < maxHops && walk != 0; i++ {
		p, ok := procs[walk]
		if !ok {
			return 0
		}
		if i > 0 && runnerNames[baseName(p.name)] {
			return int(walk)
		}
		walk = p.parent
	}
	return 0
}

// baseName strips the extension and lowercases, so claude.exe and claude both
// match the same entry.
func baseName(exe string) string {
	n := strings.ToLower(exe)
	if i := strings.LastIndex(n, "."); i > 0 {
		n = n[:i]
	}
	return n
}
