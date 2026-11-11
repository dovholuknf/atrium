package daemon

import (
	"net/http"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/hublink"
	"github.com/dovholuknf/atrium/internal/daemon/screen"
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
