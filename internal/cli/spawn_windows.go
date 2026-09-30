//go:build windows

package cli

import (
	"os"

	detachpkg "github.com/dovholuknf/atrium/internal/detach"
)

// startDetached lives in internal/detach now, so the pty host starts itself with the same flags and retry. Read the
// comment there before changing what it does.
func startDetached(exe string, args []string, out *os.File) (*os.Process, error) {
	return detachpkg.Start(exe, args, out)
}
