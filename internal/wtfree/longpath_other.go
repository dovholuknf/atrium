//go:build !windows

package wtfree

// LongPath is dir: only Windows has a path length limit to get around.
func LongPath(dir string) string { return dir }
