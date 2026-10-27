package daemon

import (
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// cardLabel is `<title>@<room>`, what a process viewer shows as the card a process belongs to. ATRIUM_CARD carries it.
func (d *Daemon) cardLabel(taskID string) string {
	title := taskID
	if t, err := d.st.Get(taskID); err == nil && t != nil && t.DisplayTitle() != "" {
		title = t.DisplayTitle()
	}
	if room := strings.TrimSpace(d.opts.Room); room != "" {
		return title + "@" + room
	}
	return title
}

// procLabel names the job object a runner or shell lives in.
func (d *Daemon) procLabel(taskID, kind string) string {
	if kind == store.RunKindShell {
		return "atrium " + d.cardLabel(taskID) + " shell"
	}
	return "atrium " + d.cardLabel(taskID)
}
