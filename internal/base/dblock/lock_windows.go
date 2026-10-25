package dblock

import (
	"errors"
	"os"

	"golang.org/x/sys/windows"
)

// The locked byte is far past the pid text, because Windows refuses reads of a locked range, and a refused opener
// has to be able to read the pid.
const lockOffset = 0x7fffff00

func lockFile(f *os.File) error {
	ol := &windows.Overlapped{Offset: lockOffset}
	return windows.LockFileEx(windows.Handle(f.Fd()), windows.LOCKFILE_EXCLUSIVE_LOCK|windows.LOCKFILE_FAIL_IMMEDIATELY, 0, 1, 0, ol)
}

func unlockFile(f *os.File) {
	ol := &windows.Overlapped{Offset: lockOffset}
	windows.UnlockFileEx(windows.Handle(f.Fd()), 0, 1, 0, ol)
}

func isHeld(err error) bool {
	return errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_IO_PENDING)
}
