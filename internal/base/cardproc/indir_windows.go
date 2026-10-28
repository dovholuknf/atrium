//go:build windows

package cardproc

import (
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/windows"
)

// InDir is every process, bar this one and atrium's own, whose working directory or executable is under dir, or whose
// command line names dir, each with its start time. A process that outlived its parent leaves no tree for Tree to
// walk, and this finds it.
func InDir(dir string) map[int]string {
	out := map[int]string{}
	if strings.TrimSpace(dir) == "" {
		return out
	}
	dir = resolved(dir)
	dir = strings.ToLower(filepath.Clean(dir))
	self, _ := os.Executable()
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return out
	}
	defer windows.CloseHandle(snap)
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		pid := int(e.ProcessID)
		if pid == os.Getpid() || pid <= 4 {
			continue
		}
		image, cwd, cmdline := procPaths(pid)
		if sameExe(image, self) || strings.HasPrefix(strings.ToLower(filepath.Base(image)), "atrium") {
			continue
		}
		if !under(image, dir) && !under(cwd, dir) && !under(resolved(image), dir) && !under(resolved(cwd), dir) && !strings.Contains(strings.ToLower(cmdline), dir) {
			continue
		}
		if st, err := StartTime(pid); err == nil {
			out[pid] = st
		}
	}
	return out
}

// resolved is p with symlinks, junctions and 8.3 short names expanded, the way InDir spells dir.
func resolved(p string) string {
	if p == "" {
		return ""
	}
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return p
	}
	// backup semantics lets CreateFile open a directory
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil, windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return p
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || n > uint32(len(buf)) {
		return p
	}
	return strings.TrimPrefix(windows.UTF16ToString(buf[:n]), `\\?\`)
}

func under(p, dir string) bool {
	p = strings.ToLower(filepath.Clean(p))
	return p != "." && (p == dir || strings.HasPrefix(p, dir+string(filepath.Separator)))
}

func sameExe(a, b string) bool {
	return a != "" && strings.EqualFold(filepath.Clean(a), filepath.Clean(b))
}

// procPaths is a process's executable, working directory and command line, each empty when the process will not say.
func procPaths(pid int) (image, cwd, cmdline string) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(pid))
	if err != nil {
		return "", "", ""
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) == nil {
		image = windows.UTF16ToString(buf[:n])
	}
	var pbi windows.PROCESS_BASIC_INFORMATION
	if windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), nil) != nil || pbi.PebBaseAddress == nil {
		return image, "", ""
	}
	var peb windows.PEB
	var params windows.RTL_USER_PROCESS_PARAMETERS
	if !readMem(h, uintptr(unsafe.Pointer(pbi.PebBaseAddress)), unsafe.Pointer(&peb), unsafe.Sizeof(peb)) || peb.ProcessParameters == nil ||
		!readMem(h, uintptr(unsafe.Pointer(peb.ProcessParameters)), unsafe.Pointer(&params), unsafe.Sizeof(params)) {
		return image, "", ""
	}
	return image, readUnicode(h, params.CurrentDirectory.DosPath), readUnicode(h, params.CommandLine)
}

func readMem(h windows.Handle, addr uintptr, into unsafe.Pointer, size uintptr) bool {
	var got uintptr
	return windows.ReadProcessMemory(h, addr, (*byte)(into), size, &got) == nil && got == size
}

func readUnicode(h windows.Handle, s windows.NTUnicodeString) string {
	if s.Length == 0 || s.Buffer == nil {
		return ""
	}
	buf := make([]uint16, s.Length/2)
	if !readMem(h, uintptr(unsafe.Pointer(s.Buffer)), unsafe.Pointer(&buf[0]), uintptr(s.Length)) {
		return ""
	}
	return windows.UTF16ToString(buf)
}

// StopInDir ends every process InDir finds, with the processes under it, and returns them for WaitGone.
func StopInDir(dir string) map[int]string {
	found := InDir(dir)
	all := map[int]string{}
	for pid := range found {
		for p, at := range Tree(pid) {
			all[p] = at
		}
	}
	for pid := range all {
		if h, err := windows.OpenProcess(windows.PROCESS_TERMINATE, false, uint32(pid)); err == nil {
			_ = windows.TerminateProcess(h, 1)
			_ = windows.CloseHandle(h)
		}
	}
	return all
}
