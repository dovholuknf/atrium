//go:build windows

package wtfree

import (
	"path/filepath"
	"strings"
)

// LongPath is dir in the form Windows takes past 260 characters: absolute, with the \\?\ prefix.
func LongPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil || strings.HasPrefix(abs, `\\?\`) {
		return dir
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}
