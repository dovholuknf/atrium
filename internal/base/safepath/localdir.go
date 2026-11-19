package safepath

import (
	"errors"
	"path/filepath"
	"strings"
)

// ErrNotLocalDir is returned for a folder name that is not a plain absolute path on this machine.
var ErrNotLocalDir = errors.New("that is not an absolute folder path on this machine")

// LocalDir is p cleaned, for a folder the operator named that has no root to be inside: absolute, no NUL, no `..`
// segment, and not a share or device path. Anything else is refused, so a bad path is never opened.
func LocalDir(p string) (string, error) {
	p = strings.TrimSpace(p)
	if p == "" || strings.IndexByte(p, 0) >= 0 || Network(p) {
		return "", ErrNotLocalDir
	}
	p = filepath.FromSlash(p)
	if !filepath.IsAbs(p) {
		return "", ErrNotLocalDir
	}
	for _, seg := range strings.Split(p, string(filepath.Separator)) {
		if seg == ".." {
			return "", ErrNotLocalDir
		}
	}
	return filepath.Clean(p), nil
}
