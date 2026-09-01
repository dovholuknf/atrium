//go:build windows

package daemon

import "golang.org/x/sys/windows"

// processAlive reports whether a pid is still running.
//
// This is a kernel question, so it costs nothing: no model turn, no token, no
// round trip to the runner. On Windows os.FindProcess always succeeds and
// Signal(0) is unsupported, so liveness has to be asked directly.
//
// PROCESS_QUERY_LIMITED_INFORMATION is the least access that can answer it and
// is granted for processes the caller could not otherwise touch.
func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	// A handle can outlive the process, so a pid with an exit code is gone even
	// though the handle opened cleanly.
	const stillActive = 259
	return code == stillActive
}
