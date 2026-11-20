//go:build windows

package cli

import (
	"os"

	"golang.org/x/sys/windows"
)

// The Go runtime writes a panic to whatever the process's stderr handle is when it dies, so swapping os.Stderr alone
// would leave the trace behind.
func setStderrHandle(f *os.File) {
	_ = windows.SetStdHandle(windows.STD_ERROR_HANDLE, windows.Handle(f.Fd()))
}
