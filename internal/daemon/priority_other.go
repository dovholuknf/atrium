//go:build !windows

package daemon

// ptyRaise is a Windows idea. Elsewhere there is no console host and this does nothing.
type ptyRaise struct{}

func (d *Daemon) beginPTYRaise() *ptyRaise { return nil }

func (r *ptyRaise) apply(pid int) {}
