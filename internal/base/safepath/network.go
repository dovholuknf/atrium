package safepath

import (
	"path/filepath"
	"strings"
)

// Network reports whether opening p could reach another machine: a UNC share, a device path, or a leading `//`.
func Network(p string) bool {
	p = strings.TrimSpace(p)
	if len(p) >= 2 && strings.ContainsRune(`/\`, rune(p[0])) && strings.ContainsRune(`/\`, rune(p[1])) {
		return true
	}
	return len(filepath.VolumeName(filepath.FromSlash(p))) > 2
}
