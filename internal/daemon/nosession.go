package daemon

import (
	"fmt"

	"github.com/dovholuknf/atrium/internal/store"
)

// A card with no session behind it: finished or dead, and no live process.
//
// A SAY TO ONE IS UNDELIVERABLE, NOT QUEUED. It used to answer `queued`, be held
// for the input line, and wear `! 1` blaming that line for as long as anybody
// cared to look, on a card with no line to wait on. Nothing can read it until
// the session is resumed, and a resumed session is the sender's to start, so the
// sender is told that now and says it again after. It is not queued as well, or
// the resumed session would get it twice.
//
// The card's own record decides, not the supervisor. A card finished by its
// report can keep a process for a while, and a runner that outlives the session
// it ran is not somebody to talk to.
func sessionGone(t *store.Task) bool {
	if t.Status != store.StatusDone && t.Status != store.StatusDead {
		return false
	}
	return t.PID <= 0 || !processAlive(t.PID)
}

// goneNote is what the sender of an undeliverable say is told.
func goneNote(t *store.Task) string {
	return fmt.Sprintf("not sent: %s has no running session (it is %s). resume it first, then say it again.",
		t.WireName, t.Status)
}
