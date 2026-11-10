//go:build windows

package cardproc

import (
	"context"
	"errors"
	"math"
	"os/exec"
	"strconv"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"

	"github.com/dovholuknf/atrium/internal/base/nowindow"
)

// StartTime is when pid started, as text that only compares equal for the same process.
func StartTime(pid int) (string, error) {
	if pid <= 0 || int64(pid) > math.MaxUint32 {
		return "", ErrGone
	}
	// codeql[go/incorrect-integer-conversion] pid is bounded to 1..MaxUint32 by the check above
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		if errors.Is(err, windows.ERROR_INVALID_PARAMETER) {
			return "", ErrGone
		}
		return "", err
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err == nil && code != 259 { // 259 is STILL_ACTIVE
		return "", ErrGone
	}
	var c, e, k, u windows.Filetime
	if err := windows.GetProcessTimes(h, &c, &e, &k, &u); err != nil {
		return "", err
	}
	return strconv.FormatInt(c.Nanoseconds(), 10), nil
}

// Tree is pid and every process under it, each with its start time, read now. Read it before StopTree, which leaves
// no parent to walk from.
func Tree(pid int) map[int]string {
	kids := map[int][]int{}
	if snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0); err == nil {
		var e windows.ProcessEntry32
		e.Size = uint32(unsafe.Sizeof(e))
		for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
			kids[int(e.ParentProcessID)] = append(kids[int(e.ParentProcessID)], int(e.ProcessID))
		}
		_ = windows.CloseHandle(snap)
	}
	return treeOf(pid, kids, func(child, parent string) bool {
		c, cerr := strconv.ParseInt(child, 10, 64)
		p, perr := strconv.ParseInt(parent, 10, 64)
		return cerr != nil || perr != nil || c >= p
	})
}

// OwnGroup starts cmd in a process group of its own, so a Ctrl+C to atrium's console does not reach it. StopTree
// walks the tree by parent, so nothing more is needed for that.
func OwnGroup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_PROCESS_GROUP}
}

// StopTree ends pid and every process it started, through taskkill /T, which walks the tree by parent.
func StopTree(pid int) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "taskkill", "/T", "/F", "/PID", strconv.Itoa(pid))
	nowindow.Hide(cmd)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if _, serr := StartTime(pid); errors.Is(serr, ErrGone) {
			return nil
		}
		if terminateTree(pid) == nil {
			return nil
		}
		return errors.New("taskkill: " + string(out))
	}
	return nil
}

// terminateTree ends pid and its tree with TerminateProcess, for a token taskkill.exe is denied on while the handle is
// not. A pid whose start time changed since Tree read it is someone else's and is left alone.
func terminateTree(pid int) error {
	var first error
	for p, at := range Tree(pid) {
		if now, err := StartTime(p); err != nil || now != at {
			continue
		}
		// codeql[go/incorrect-integer-conversion] p came from a process snapshot, so it fits a uint32
		h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, uint32(p))
		if err != nil {
			if first == nil {
				first = err
			}
			continue
		}
		if err := windows.TerminateProcess(h, 1); err != nil && first == nil {
			first = err
		} else if err == nil {
			_, _ = windows.WaitForSingleObject(h, 5000)
		}
		_ = windows.CloseHandle(h)
	}
	if _, err := StartTime(pid); !errors.Is(err, ErrGone) && first == nil {
		first = errors.New("still running")
	}
	return first
}

// Excluded is the TCP port ranges Windows has reserved (Hyper-V, WSL). A bind there fails with WSAEACCES, and a
// loopback probe can pass on one before it does.
func Excluded() []Range {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "netsh", "int", "ipv4", "show", "excludedportrange", "protocol=tcp")
	nowindow.Hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return parseExcluded(string(out))
}

// Ancestors is pid's parent, its parent's parent and so on, up to hops of them, each with its executable's name.
// Empty when pid is not running.
func Ancestors(pid, hops int) []Proc {
	type entry struct {
		name   string
		parent int
	}
	procs := map[int]entry{}
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil
	}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		procs[int(e.ProcessID)] = entry{name: windows.UTF16ToString(e.ExeFile[:]), parent: int(e.ParentProcessID)}
	}
	_ = windows.CloseHandle(snap)
	return ancestorsOf(pid, hops, func(p int) (string, int, bool) {
		en, ok := procs[p]
		return en.name, en.parent, ok
	})
}
