//go:build !windows

package main

// raisePriority is Windows only. See priority_windows.go.
func raisePriority() {}
