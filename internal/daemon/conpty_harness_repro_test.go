//go:build windows

package daemon

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// scratch harness for backlog-2 item 74. not for commit into anything that ships.
//
// A pseudo console opened through either the inbox kernel32 (dll == "") or a
// conpty.dll named by path, which starts the OpenConsole.exe beside it. The two
// paths share every other line, so a difference in what comes out is the
// console host's and nothing else's.
type harnessPTY struct {
	in, out *os.File
	hpc     windows.Handle
	proc    windows.Handle
	resize  func(cols, rows int) error
	close   func()
}

func (h *harnessPTY) Read(b []byte) (int, error)  { return h.out.Read(b) }
func (h *harnessPTY) Write(b []byte) (int, error) { return h.in.Write(b) }
func (h *harnessPTY) Resize(cols, rows int) error { return h.resize(cols, rows) }
func (h *harnessPTY) Close()                      { h.close() }
func (h *harnessPTY) Wait() {
	_, _ = windows.WaitForSingleObject(h.proc, windows.INFINITE)
}
func (h *harnessPTY) Kill() { _ = windows.TerminateProcess(h.proc, 1) }

func openHarnessPTY(dll string, cols, rows int, args []string, dir string, env []string) (*harnessPTY, error) {
	create := func(size windows.Coord, in, out windows.Handle, hpc *windows.Handle) error {
		return windows.CreatePseudoConsole(size, in, out, 0, hpc)
	}
	resize := func(hpc windows.Handle, size windows.Coord) error { return windows.ResizePseudoConsole(hpc, size) }
	closePC := func(hpc windows.Handle) { windows.ClosePseudoConsole(hpc) }
	if dll != "" {
		d, err := windows.LoadDLL(dll)
		if err != nil {
			return nil, err
		}
		pc, pr, pl := d.MustFindProc("CreatePseudoConsole"), d.MustFindProc("ResizePseudoConsole"),
			d.MustFindProc("ClosePseudoConsole")
		create = func(size windows.Coord, in, out windows.Handle, hpc *windows.Handle) error {
			r, _, _ := pc.Call(uintptr(*(*uint32)(unsafe.Pointer(&size))), uintptr(in), uintptr(out), 0,
				uintptr(unsafe.Pointer(hpc)))
			if r != 0 {
				return fmt.Errorf("CreatePseudoConsole: 0x%x", r)
			}
			return nil
		}
		resize = func(hpc windows.Handle, size windows.Coord) error {
			r, _, _ := pr.Call(uintptr(hpc), uintptr(*(*uint32)(unsafe.Pointer(&size))))
			if r != 0 {
				return fmt.Errorf("ResizePseudoConsole: 0x%x", r)
			}
			return nil
		}
		closePC = func(hpc windows.Handle) { _, _, _ = pl.Call(uintptr(hpc)) }
	}
	ptyIn, inOurs, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	outOurs, ptyOut, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	var hpc windows.Handle
	if err := create(windows.Coord{X: int16(cols), Y: int16(rows)}, windows.Handle(ptyIn.Fd()),
		windows.Handle(ptyOut.Fd()), &hpc); err != nil {
		return nil, err
	}
	_ = ptyIn.Close()
	_ = ptyOut.Close()

	attrs, err := windows.NewProcThreadAttributeList(1)
	if err != nil {
		return nil, err
	}
	if err := attrs.Update(0x20016, unsafe.Pointer(hpc), unsafe.Sizeof(hpc)); err != nil {
		return nil, err
	}
	si := &windows.StartupInfoEx{ProcThreadAttributeList: attrs.List()}
	si.Cb = uint32(unsafe.Sizeof(*si))
	si.Flags = windows.STARTF_USESTDHANDLES
	cmdline, err := windows.UTF16PtrFromString(windows.ComposeCommandLine(args))
	if err != nil {
		return nil, err
	}
	var dirp *uint16
	if dir != "" {
		dirp, _ = windows.UTF16PtrFromString(dir)
	}
	var block []uint16
	for _, kv := range env {
		block = append(block, windows.StringToUTF16(kv)...)
	}
	block = append(block, 0)
	var pi windows.ProcessInformation
	if err := windows.CreateProcess(nil, cmdline, nil, nil, false,
		windows.EXTENDED_STARTUPINFO_PRESENT|windows.CREATE_UNICODE_ENVIRONMENT, &block[0], dirp,
		&si.StartupInfo, &pi); err != nil {
		return nil, fmt.Errorf("CreateProcess %s: %w", strings.Join(args, " "), err)
	}
	_ = windows.CloseHandle(pi.Thread)
	attrs.Delete()
	h := &harnessPTY{in: inOurs, out: outOurs, hpc: hpc, proc: pi.Process}
	h.resize = func(c, r int) error { return resize(hpc, windows.Coord{X: int16(c), Y: int16(r)}) }
	// Once: a second close of a pseudo console handle ends the process silently.
	var once sync.Once
	h.close = func() {
		once.Do(func() {
			closePC(hpc)
			_ = inOurs.Close()
			_ = outOurs.Close()
		})
	}
	return h, nil
}
