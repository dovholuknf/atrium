package daemon

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/dovholuknf/atrium/internal/store"
)

// `atrium_clear_context`: a session asking for its own context to be cleared, through POST /v1/tasks/{id}/new-context
// with a `handoff` in the body (handleNewContext).
//
// It enters the new-context cycle (newcontext.go) already past the ack: the handoff text is the ack. The run then waits
// for the turn to end, types `/clear`, and types a wake telling the new session to re-read its BRIEF.md and the
// handoff. The call answers at once, and nothing is typed mid-turn.

// ClearContextLine is what the tool answers when the clear is queued.
const ClearContextLine = "atrium clears your context when this turn ends, then tells the new session to read your handoff. End your turn now."

var errNotClaude = errors.New("atrium types /clear only into a claude session, and this card's runner is not claude")

// clearContextWake is what the new session is told: its brief, then the handoff.
func clearContextWake(brief, path string) string {
	if brief == "" {
		return "your context was cleared at your own request. read " + path + " and continue."
	}
	return "your context was cleared at your own request. re-read " + brief + ", then read the handoff at " + path + " and continue."
}

// briefOf is the absolute path of the card's BRIEF.md, in its home or its worktree, "" when it has none.
func (d *Daemon) briefOf(t *store.Task) string {
	var dirs []string
	if h, ok := d.homeOf(t.ID); ok {
		dirs = append(dirs, dirsOf(h.Path).Dir)
	}
	if t.Worktree != "" {
		dirs = append(dirs, filepath.FromSlash(t.Worktree))
	}
	for _, dir := range dirs {
		p := filepath.Join(dir, homeBrief)
		if info, err := os.Stat(p); err == nil && !info.IsDir() {
			return filepath.ToSlash(p)
		}
	}
	return ""
}

// ClearContext writes the handoff and queues the clear, returning at once.
func (d *Daemon) ClearContext(task *store.Task, handoff string) (string, int, error) {
	text := handoff
	if strings.TrimSpace(text) == "" {
		return "", http.StatusConflict, errString("the handoff is empty: write what a fresh session needs to carry on")
	}
	h, _ := d.harnessOf(task.ID)
	if !isClaude(h) && !(h == nil && strings.EqualFold(task.Runner, "claude")) {
		return "", http.StatusConflict, errNotClaude
	}
	if d.sup.get(task.ID) == nil {
		return "", http.StatusConflict, errNoTerminal
	}
	if other := d.nctx.sameDirBusy(d, task); other != "" {
		return "", http.StatusConflict, &newContextSharedError{other: other}
	}
	if len(text) > handoffKeep {
		text = text[:handoffKeep]
		for !utf8.ValidString(text) {
			text = text[:len(text)-1]
		}
	}
	path, err := d.prepareHandoff(task)
	if err != nil {
		return "", http.StatusInternalServerError, err
	}
	c := &newContext{step: NewContextClear, conv: d.conversationOf(task), file: path, acked: true,
		wake: clearContextWake(d.briefOf(task), filepath.ToSlash(path))}
	if cur := d.nctx.get(task.ID); cur != nil && cur.step != NewContextFailed {
		return "", http.StatusConflict, fmt.Errorf("%w, on step %s", errNewContextBusy, cur.step)
	}
	if err := os.WriteFile(path, []byte(text), 0o644); err != nil {
		return "", http.StatusInternalServerError, fmt.Errorf("could not write the handoff to %s: %w", path, err)
	}
	gen, ok := d.nctx.claim(task.ID, c)
	if !ok {
		return "", http.StatusConflict, errNewContextBusy
	}
	if err := d.st.AppendEvent(task.ID, store.EventNotified,
		map[string]any{"by": newContextBy, "handoff": text, "path": path, "bytes": len(text)}); err != nil {
		log.Printf("[atrium] could not store the handoff on %s: %v", task.ID, err)
	}
	log.Printf("[atrium] clear context asked by %s, %d bytes in %s", task.DisplayTitle(), len(text), path)
	d.publishTask(task.ID)
	go d.runNewContext(task.ID, gen)
	return path, 0, nil
}
