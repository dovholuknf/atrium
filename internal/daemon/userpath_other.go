//go:build !windows

package daemon

// currentUserPath has no registry to read off Windows, where the process PATH is the one to use.
func currentUserPath() (string, bool) { return "", false }
