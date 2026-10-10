//go:build windows

package api

import (
	"path/filepath"
	"strings"
)

// longPath is dir in the form Windows takes past 260 characters: absolute, with the \\?\ prefix.
func longPath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil || strings.HasPrefix(abs, `\\?\`) {
		return dir
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}
