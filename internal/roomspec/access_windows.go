//go:build windows

package roomspec

import "golang.org/x/sys/windows"

// canWriteDir opens the folder asking for the right to add a file and a subfolder. The open succeeds only when the ACL grants
// both to this login, and it writes nothing, which is why the plan can use it and a probe file could not be used.
func canWriteDir(dir string) bool {
	p, err := windows.UTF16PtrFromString(dir)
	if err != nil {
		return false
	}
	const fileAddFile, fileAddSubdirectory = 0x2, 0x4
	h, err := windows.CreateFile(p, fileAddFile|fileAddSubdirectory,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE|windows.FILE_SHARE_DELETE, nil,
		windows.OPEN_EXISTING, windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return false
	}
	_ = windows.CloseHandle(h)
	return true
}
