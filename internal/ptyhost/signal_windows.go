//go:build windows

package ptyhost

import "os"

// termProcess is `signal term`. Windows has no graceful signal for a process on a ConPTY that the host can send:
// os.Process.Signal supports only Kill. So term is a kill here. A daemon that wants a polite stop types the
// runner's exit keys through `write`, as its windDown does today, and uses `signal` only when that has failed.
func termProcess(p *os.Process) error { return p.Kill() }
