//go:build !windows

package ptyhost

// raise is Windows only. See raise_windows.go.
type raise struct{}

func beginRaise() *raise { return nil }

func (r *raise) apply(int) error { return nil }
