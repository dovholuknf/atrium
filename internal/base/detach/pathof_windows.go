//go:build windows

package detach

import (
	"os"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// The handle is asked because os.File.Name is what the file was opened as, "/dev/stderr" for a redirected stderr.
func PathOf(f *os.File) (string, bool) {
	if f == nil {
		return "", false
	}
	if fi, err := f.Stat(); err != nil || !fi.Mode().IsRegular() {
		return "", false
	}
	var buf [32768]uint16
	n, err := windows.GetFinalPathNameByHandle(windows.Handle(f.Fd()), &buf[0], uint32(len(buf)), 0)
	if err != nil || n == 0 || int(n) >= len(buf) {
		return "", false
	}
	p := syscall.UTF16ToString(buf[:n])
	if strings.HasPrefix(p, `\\?\UNC\`) {
		return `\\` + strings.TrimPrefix(p, `\\?\UNC\`), true
	}
	return strings.TrimPrefix(p, `\\?\`), true
}
