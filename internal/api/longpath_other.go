//go:build !windows

package api

// longPath is dir as it is: only Windows has a path length limit to get past.
func longPath(dir string) string { return dir }
