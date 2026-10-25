//go:build !windows

package userenv

func current() map[string]string { return nil }
