//go:build !windows

package cli

// raisePriority is Windows only. See priority_windows.go.
func raisePriority() {}
