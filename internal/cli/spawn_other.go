//go:build !windows

package cli

import (
	"os"

	detachpkg "github.com/dovholuknf/atrium/internal/detach"
)

// startDetached lives in internal/detach now. See internal/detach/detach_other.go.
func startDetached(exe string, args []string, out *os.File) (*os.Process, error) {
	return detachpkg.Start(exe, args, out)
}
