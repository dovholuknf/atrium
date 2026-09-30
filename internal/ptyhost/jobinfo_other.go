//go:build !windows

package ptyhost

// inJob is Windows only, and always false elsewhere. See jobinfo_windows.go.
func inJob() (bool, string) { return false, "not applicable" }
