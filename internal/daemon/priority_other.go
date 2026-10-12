//go:build !windows

package daemon

// ptyRaise is a Windows idea. Elsewhere there is no console host and this does nothing.
type ptyRaise struct{}

func (d *Daemon) beginPTYRaise() *ptyRaise { return nil }

// platformHooks has nothing to add where no priority is raised.
func platformHooks(*hooks) {}

func (r *ptyRaise) apply(pid int) {}
