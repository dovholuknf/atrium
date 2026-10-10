package daemon

import (
	"context"
	"strings"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/store"
)

// visibleLauncher is the launcher's card on THIS room when the launcher's directory and the one at dir are in one
// repository (same git common dir): what one commits there the other sees at once. Best effort: an unknown launcher,
// one on another room or any git error is nil.
func (d *Daemon) visibleLauncher(ctx context.Context, worker *store.Task, byID, byName, dir string) *store.Task {
	var l *store.Task
	if byID = strings.TrimSpace(byID); byID != "" {
		if t, err := d.st.Get(byID); err == nil {
			l = t
		}
	}
	if l == nil && strings.TrimSpace(byName) != "" {
		l = d.localTarget(strings.TrimSpace(byName))
	}
	if l == nil || l.WireName == "" || (worker != nil && l.ID == worker.ID) || strings.TrimSpace(l.Worktree) == "" {
		return nil
	}
	if !gitsync.SharesGitDir(ctx, gitsync.Default, dir, l.Worktree) {
		return nil
	}
	return l
}

// commitVisibleLine tells a worker its commits are already where its launcher reads them.
func commitVisibleLine(handle string) string {
	return "Your commits are already visible to " + handle + " (same repository on this machine), so do not push."
}

// withCommitVisible puts line ahead of the launch ending, which stays the last text of the prompt.
func withCommitVisible(prompt, line string) string {
	if line == "" {
		return prompt
	}
	if i := strings.LastIndex(prompt, LaunchEndingLine); i >= 0 {
		return prompt[:i] + line + "\n\n" + prompt[i:]
	}
	return strings.TrimRight(prompt, "\n") + "\n\n" + line
}
