//go:build windows

package daemon

import "github.com/dovholuknf/atrium/internal/base/cardproc"

// killTree ends pid and what it started. Best effort: the caller still kills pid itself.
func killTree(pid int) { _ = cardproc.StopTree(pid) }
