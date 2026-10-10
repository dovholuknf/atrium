//go:build integration && windows

package daemon

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"unsafe"

	"golang.org/x/sys/windows"
)

// harnessViaPTY is a runner's pty whose resizes land on a harness pseudo
// console, marked at the byte they happened at. Everything else is the fake's.
type harnessViaPTY struct {
	*fakePTY
	p    *harnessPTY
	mark func(cols, rows int)
	n    *int
}

func (h *harnessViaPTY) Resize(cols, rows int) error {
	h.mark(cols, rows)
	*h.n++
	return h.p.Resize(cols, rows)
}

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
	// The attribute's value IS the handle, so its bits go in as the pointer.
	if err := attrs.Update(0x20016, *(*unsafe.Pointer)(unsafe.Pointer(&hpc)), unsafe.Sizeof(hpc)); err != nil {
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
