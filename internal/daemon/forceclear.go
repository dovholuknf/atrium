package daemon

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/daemon/screen"
	"github.com/dovholuknf/atrium/internal/store"
)

// Where the text a notice shows came from: atrium's own typing model, or the prompt row read off the screen.
const (
	lineFromModel  = "model"
	lineFromScreen = "screen"
)

// lineShown is what to show a person as the text blocking a message. The typing model when it is exact, because then
// it is the whole line character for character. Otherwise the prompt row on screen, which is one row of an idle Claude
// prompt and so can be cut short. Otherwise nothing, and the source is empty.
func (r *runner) lineShown(s typingState) (string, string) {
	if s.Exact && s.Line != "" {
		return s.Line, lineFromModel
	}
	if r.buf == nil {
		return "", ""
	}
	cols, rows := r.buf.CurrentSize()
	if t := screen.FramePromptText(r.buf.Tail(screen.FrameTailBytes), cols, rows); t != "" {
		return t, lineFromScreen
	}
	return "", ""
}

// handleForceClear is the phone's "Force clear now": throw away whatever is on the card's input line so the messages
// held for it go in. Control-c is what a Claude Code prompt takes as discard this draft. It is refused mid-turn, where
// control-c would interrupt the work, and under a dialog or a new-context cycle, which own the terminal, and while a key
// is still landing, so it never clears under somebody's hands. The held messages are retried at once.
func (d *Daemon) handleForceClear(w http.ResponseWriter, r *http.Request) {
	taskID := r.PathValue("id")
	run := d.sup.get(taskID)
	if run == nil {
		hublink.WriteJSONCode(w, http.StatusNotFound, hublink.ErrBody("no supervised terminal on this card"))
		return
	}
	switch {
	case d.HoldingMessages(taskID):
		hublink.WriteJSONCode(w, http.StatusConflict, hublink.ErrBody("a new-context cycle owns that terminal, so nothing was cleared"))
		return
	case d.act.dialogOpen(taskID):
		hublink.WriteJSONCode(w, http.StatusConflict, hublink.ErrBody("a dialog is on that terminal, so nothing was cleared"))
		return
	case d.act.midTurn(taskID):
		hublink.WriteJSONCode(w, http.StatusConflict, hublink.ErrBody("the card is mid-turn, and clearing would interrupt it"))
		return
	}
	run.injectMu.Lock()
	defer run.injectMu.Unlock()
	run.pasteMu.Lock()
	defer run.pasteMu.Unlock()
	run.typeMu.Lock()
	if !run.lastTyped.IsZero() && time.Since(run.lastTyped) < letThroughQuiet {
		run.typeMu.Unlock()
		hublink.WriteJSONCode(w, http.StatusConflict, hublink.ErrBody("a key was just pressed on that line, so nothing was cleared"))
		return
	}
	run.typeMu.Unlock()
	if err := run.Write([]byte{0x03}); err != nil {
		hublink.WriteJSONCode(w, http.StatusInternalServerError, hublink.ErrBody(err.Error()))
		return
	}
	run.typeMu.Lock()
	run.line.clear()
	run.typeMu.Unlock()
	if d.pending != nil {
		d.pending.reset(taskID)
	}
	d.Publish(taskID)
	hublink.WriteJSONCode(w, http.StatusOK, map[string]any{"cleared": true})
}

// staleLineAfter is how long unsent text may sit on a line nobody types into before a new context or a held say
// clears it and goes on.
const staleLineAfter = 5 * time.Minute

// clearStaleLine throws away text left on the card's line that nobody has typed into for staleLineAfter, so a new
// context or a held message is not stuck behind a stray character. The text is saved first, on the card's events and
// in a file, and the board is told once with it, so it can be put back. Refused under a dialog and mid-turn, as the
// force clear is. Reports whether it cleared.
func (d *Daemon) clearStaleLine(taskID string) bool {
	run := d.sup.get(taskID)
	if run == nil || d.act.dialogOpen(taskID) || d.act.midTurn(taskID) {
		return false
	}
	run.injectMu.Lock()
	defer run.injectMu.Unlock()
	run.pasteMu.Lock()
	defer run.pasteMu.Unlock()
	s := run.typing()
	if s.Count == 0 && s.Unsure == "" {
		return false
	}
	run.typeMu.Lock()
	last := run.lastTyped
	run.typeMu.Unlock()
	if last.IsZero() {
		last = run.started
		if run.reattachedAt.After(last) {
			last = run.reattachedAt
		}
	}
	quiet := time.Since(last)
	if quiet < staleLineAfter {
		return false
	}
	text, from := run.lineShown(s)
	if err := run.Write([]byte{0x03}); err != nil {
		log.Printf("[atrium] could not clear the stale line on %s: %v", taskID, err)
		return false
	}
	run.typeMu.Lock()
	run.line.clear()
	run.typeMu.Unlock()
	if d.pending != nil {
		d.pending.reset(taskID)
	}
	d.keepClearedLine(taskID, text, from, quiet)
	return true
}

// keepClearedLine saves a cleared line under the card's folder beside the database and on its events, and rings the
// board's notice with it.
func (d *Daemon) keepClearedLine(taskID, text, from string, quiet time.Duration) {
	shown := text
	if shown == "" {
		shown = "text atrium could not read"
	}
	msg := fmt.Sprintf("You had '%s' on the line and did not send it within %d minutes, so I cleared it.",
		shown, int(staleLineAfter.Minutes()))
	file := ""
	dir := filepath.Join(filepath.Dir(d.opts.DBPath), "cards", taskID, "cleared-lines")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		log.Printf("[atrium] could not keep the cleared line of %s: %v", taskID, err)
	} else {
		file = filepath.ToSlash(filepath.Join(dir, time.Now().UTC().Format("20060102-150405.000")+".txt"))
		if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
			log.Printf("[atrium] could not keep the cleared line of %s: %v", taskID, err)
			file = ""
		}
	}
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
		"by": lineClearedBy, "text": msg, "line": text, "line_from": from, "file": file, "quiet_s": int(quiet.Seconds()),
	}); err != nil {
		log.Printf("[atrium] could not record the cleared line on %s: %v", taskID, err)
	}
	log.Printf("[atrium] %s: cleared %d character(s) left on the line for %s, kept in %s", taskID, len([]rune(text)),
		quiet.Round(time.Second), file)
	d.Publish(taskID)
	d.ap.Broadcast("notice", map[string]any{
		"task_id": taskID, "about_card": taskID, "source": lineClearedBy, "toast": msg, "line": text, "file": file,
	})
}

// lineClearedBy names a cleared line, as the notified event's author and the notice's source.
const lineClearedBy = "line-cleared"
