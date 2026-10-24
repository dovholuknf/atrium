//go:build !windows

package cli

import (
	"os"

	detachpkg "github.com/dovholuknf/atrium/internal/base/detach"
)

func startDetached(exe string, args []string, log string) (*os.Process, error) {
	return detachpkg.Start(exe, args, log)
}
