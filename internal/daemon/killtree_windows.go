//go:build windows

package daemon

import (
	"strconv"
	"time"

	"github.com/dovholuknf/atrium/internal/base/cardproc"
)

// killTree ends pid and what it started. Best effort: the caller still kills pid itself.
func killTree(pid int) { _ = cardproc.StopTree(pid) }

// reapLeft ends what an exited runner left running, its MCP servers among them. A child of a dead pid still names it as
// its parent, so the pid finds them.
func reapLeft(pid int, started time.Time) {
	if pid <= 4 || started.IsZero() {
		return
	}
	cardproc.StopOrphans(pid, strconv.FormatInt(started.UnixNano(), 10))
}
