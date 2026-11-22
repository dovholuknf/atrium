package cardroot

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// dataDrives is every fixed drive but the system drive, in letter order, as `D:\`.
func dataDrives() []string {
	// A guarded test keeps its homes under the LOCALAPPDATA testguard moved, never on a real drive.
	if os.Getenv("ATRIUM_TESTGUARD") == "1" {
		return nil
	}
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	sys := strings.TrimSpace(os.Getenv("SystemDrive"))
	if sys == "" {
		sys = "C:"
	}
	var out []string
	for i := 0; i < 26; i++ {
		if mask&(1<<uint(i)) == 0 {
			continue
		}
		root := string(rune('A'+i)) + `:\`
		if strings.EqualFold(root[:2], sys) {
			continue
		}
		p, err := windows.UTF16PtrFromString(root)
		if err != nil || windows.GetDriveType(p) != windows.DRIVE_FIXED {
			continue
		}
		out = append(out, root)
	}
	return out
}
