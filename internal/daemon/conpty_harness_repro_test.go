//go:build windows

package daemon

import (
	"os"

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
