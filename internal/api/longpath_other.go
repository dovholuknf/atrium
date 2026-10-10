//go:build !windows

package api

import "github.com/dovholuknf/atrium/internal/wtfree"

// longPath is dir as it is: only Windows has a path length limit to get past.
func longPath(dir string) string { return wtfree.LongPath(dir) }
