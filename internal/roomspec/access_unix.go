//go:build !windows

package roomspec

import "golang.org/x/sys/unix"

// canWriteDir asks the kernel whether this process may make a file in dir (write and search permission). access(2) writes
// nothing.
func canWriteDir(dir string) bool { return unix.Access(dir, unix.W_OK|unix.X_OK) == nil }
