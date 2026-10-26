//go:build windows

package roomstats

import (
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// procWindow is how long CPU is measured over. Windows keeps no recent rate.
const procWindow = time.Second

type winProc struct {
	p       Proc
	created time.Time
	cpu     time.Duration
}

// ListTop is the n busiest processes over procWindow, with each one's command
// line and cwd read from its PEB. A process that refuses to be opened is left
// out: it is not one atrium could have started.
func ListTop(n int) ([]Proc, error) {
	first, err := winProcs()
	if err != nil {
		return nil, err
	}
	time.Sleep(procWindow)
	second, err := winProcs()
	if err != nil {
		return nil, err
	}
	var ps []Proc
	for pid, b := range second {
		a, ok := first[pid]
		if !ok || !a.created.Equal(b.created) || b.cpu < a.cpu {
			continue
		}
		b.p.CPUPct = round1(float64(b.cpu-a.cpu) / float64(procWindow) * 100)
		ps = append(ps, b.p)
	}
	ps = TopByCPU(ps, n)
	for i := range ps {
		readPEB(&ps[i])
		if r := []rune(ps[i].Command); len(r) > cmdMax {
			ps[i].Command = string(r[:cmdMax])
		}
	}
	return ps, nil
}

// ProcByPID is one process with its command line and cwd. ok is false when it
// is gone or cannot be opened.
func ProcByPID(pid int) (Proc, bool, error) {
	all, err := winProcs()
	if err != nil {
		return Proc{}, false, err
	}
	w, ok := all[pid]
	if !ok {
		return Proc{}, false, nil
	}
	readPEB(&w.p)
	return w.p, true, nil
}

func winProcs() (map[int]winProc, error) {
	snap, err := windows.CreateToolhelp32Snapshot(windows.TH32CS_SNAPPROCESS, 0)
	if err != nil {
		return nil, err
	}
	defer windows.CloseHandle(snap)
	out := map[int]winProc{}
	var e windows.ProcessEntry32
	e.Size = uint32(unsafe.Sizeof(e))
	now := time.Now()
	for err = windows.Process32First(snap, &e); err == nil; err = windows.Process32Next(snap, &e) {
		h, oerr := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, e.ProcessID)
		if oerr != nil {
			continue
		}
		var c, x, k, u windows.Filetime
		terr := windows.GetProcessTimes(h, &c, &x, &k, &u)
		windows.CloseHandle(h)
		if terr != nil {
			continue
		}
		ticks := func(f windows.Filetime) uint64 { return uint64(f.HighDateTime)<<32 | uint64(f.LowDateTime) }
		created := time.Unix(0, c.Nanoseconds())
		out[int(e.ProcessID)] = winProc{
			p: Proc{PID: int(e.ProcessID), PPID: int(e.ParentProcessID), Etime: etime(now.Sub(created)),
				Command: windows.UTF16ToString(e.ExeFile[:])},
			created: created, cpu: time.Duration(ticks(k)+ticks(u)) * 100,
		}
	}
	return out, nil
}

// readPEB fills the command line and cwd where the process lets us read its
// memory, and leaves the image name otherwise.
func readPEB(p *Proc) {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_INFORMATION|windows.PROCESS_VM_READ, false, uint32(p.PID))
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	var pbi windows.PROCESS_BASIC_INFORMATION
	if windows.NtQueryInformationProcess(h, windows.ProcessBasicInformation, unsafe.Pointer(&pbi), uint32(unsafe.Sizeof(pbi)), nil) != nil {
		return
	}
	var peb windows.PEB
	if !readMem(h, uintptr(unsafe.Pointer(pbi.PebBaseAddress)), unsafe.Pointer(&peb), unsafe.Sizeof(peb)) {
		return
	}
	var params windows.RTL_USER_PROCESS_PARAMETERS
	if !readMem(h, uintptr(unsafe.Pointer(peb.ProcessParameters)), unsafe.Pointer(&params), unsafe.Sizeof(params)) {
		return
	}
	if s, ok := readUnicode(h, params.CommandLine); ok && s != "" {
		p.Command = s
	}
	if s, ok := readUnicode(h, params.CurrentDirectory.DosPath); ok {
		p.Cwd = s
	}
}

func readMem(h windows.Handle, at uintptr, into unsafe.Pointer, n uintptr) bool {
	if at == 0 {
		return false
	}
	return windows.ReadProcessMemory(h, at, (*byte)(into), n, nil) == nil
}

func readUnicode(h windows.Handle, s windows.NTUnicodeString) (string, bool) {
	if s.Length == 0 {
		return "", true
	}
	buf := make([]uint16, s.Length/2)
	if !readMem(h, uintptr(unsafe.Pointer(s.Buffer)), unsafe.Pointer(&buf[0]), uintptr(s.Length)) {
		return "", false
	}
	return windows.UTF16ToString(buf), true
}
