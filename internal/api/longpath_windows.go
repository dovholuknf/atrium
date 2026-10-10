//go:build windows

package api

import "github.com/dovholuknf/atrium/internal/wtfree"

// longPath is dir in the form Windows takes past 260 characters: absolute, with the \\?\ prefix.
func longPath(dir string) string { return wtfree.LongPath(dir) }
