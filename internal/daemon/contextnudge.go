package daemon

import (
	"fmt"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

// A card over its context threshold, mid-turn, is told once at its next tool call.
// See docs/rnd/held-message-escalation-design.md, section 4.

// NoticeContextTold is the launcher's notice that a card was told about its size twice.
const NoticeContextTold = "context-told"

// contextToldStep is how much a card must grow past the size it was told at to be told again.
const contextToldStep = 50_000

// contextTold is the claim on one card: the turn it was told in, the size it was last
// told at, and how many times. IN MEMORY, like the size: a restart tells a card at most
// once more.
type contextTold struct {
	turn   string
	tokens int64
	n      int
}

// turnKey names the turn a card is in: when it opened, or the prompt that started it.
func (d *Daemon) turnKey(t *store.Task) string {
	if began, ok := d.act.turnSince(t.ID); ok {
		return began.UTC().Format(time.RFC3339Nano)
	}
	return t.PromptKey()
}

// contextLine is the line a running card over its threshold gets in front of its next
// tool call, or "". Once per turn, once more at +50k with the launcher told, then
// nothing. The claim is taken here, so a call that asks is a call that tells.
func (d *Daemon) contextLine(t *store.Task) string {
	if t.Status != store.StatusRunning {
		return ""
	}
	d.ctx.mu.Lock()
	seen, ok := d.ctx.m[t.ID]
	d.ctx.mu.Unlock()
	if !ok || seen.tokens < api.ContextThreshold(d.st) {
		return ""
	}
	turn := d.turnKey(t)
	d.ctx.mu.Lock()
	was := d.ctx.told[t.ID]
	switch {
	case was.turn != turn:
		was = contextTold{turn: turn, tokens: seen.tokens, n: 1}
	case was.n == 1 && seen.tokens >= was.tokens+contextToldStep:
		was = contextTold{turn: turn, tokens: seen.tokens, n: 2}
	default:
		d.ctx.mu.Unlock()
		return ""
	}
	d.ctx.told[t.ID] = was
	d.ctx.mu.Unlock()

	line := fmt.Sprintf("[atrium] you are at %dk context. finish the step you are on, commit, write your handoff, "+
		"and end your turn.", seen.tokens/1000)
	if d.autoContextSubject(t, d.st.AutoNewContextMode()) {
		line += " atrium cycles your context when the turn ends."
	}
	if was.n == 2 && d.reportsToLauncher(t) {
		d.notifyLauncher(t, NoticeContextTold, turn, fmt.Sprintf(
			"%s is still in one turn at %dk context and has been told twice to wrap up. card %s",
			t.WireName, seen.tokens/1000, t.ID))
	}
	return line
}

// withContextLine puts the context line in front of a block reason, or stands alone.
func withContextLine(line, reason string) string {
	if line == "" {
		return reason
	}
	if strings.TrimSpace(reason) == "" {
		return line + " This tool call was not refused on its merits: it was interrupted to reach you. " +
			"Retry it if it still makes sense."
	}
	return line + "\n\n" + reason
}
