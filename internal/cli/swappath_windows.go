//go:build windows

package cli

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

// EvalSymlinks lists every ancestor, so it is denied under a folder this user may pass through but not list.
func swapRealPath(p string) (string, error) {
	u, err := windows.UTF16PtrFromString(p)
	if err != nil {
		return "", err
	}
	h, err := windows.CreateFile(u, 0, windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return "", err
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetFinalPathNameByHandle(h, &buf[0], uint32(len(buf)), 0)
	if err != nil {
		return "", err
	}
	if n == 0 || n >= uint32(len(buf)) {
		return "", windows.ERROR_INSUFFICIENT_BUFFER
	}
	r := windows.UTF16ToString(buf[:n])
	if strings.HasPrefix(r, `\\?\UNC\`) {
		return filepath.Clean(`\\` + strings.TrimPrefix(r, `\\?\UNC\`)), nil
	}
	return filepath.Clean(strings.TrimPrefix(r, `\\?\`)), nil
}
