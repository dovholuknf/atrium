package daemon

import (
	"fmt"
	"log"
	"strconv"
	"strings"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// A `when: done` message stops waiting for the turn after escalate_held_after.
// See docs/rnd/held-message-escalation-design.md, section 3.

// heldAfterDefault is the wait when the setting is empty.
const heldAfterDefault = 15 * time.Minute

// EscalatedBy names the card event that records an escalated message.
const EscalatedBy = "held-escalation"

// heldAfter reads escalate_held_after, in minutes. A bad value or a read failure
// answers the default.
func (d *Daemon) heldAfter() time.Duration {
	v, err := d.st.Setting(store.SettingEscalateHeldAfter)
	if err != nil {
		return heldAfterDefault
	}
	n, err := strconv.Atoi(strings.TrimSpace(v))
	if err != nil || n <= 0 {
		return heldAfterDefault
	}
	return time.Duration(n) * time.Minute
}

// heldAged reports whether a message queued at `at` has waited past the setting
// and may leave the turn wait. A card whose deploy wake is still to be typed never
// counts: an old message must not land ahead of the wake.
func (d *Daemon) heldAged(taskID string, at time.Time) bool {
	if at.IsZero() || time.Since(at) < d.heldAfter() {
		return false
	}
	return !d.holds.awaitingWake(taskID, time.Now())
}

// escalatesByTyping is true when the typist may type an aged message mid-turn: the
// runner takes input mid-turn, and the message has waited long enough. A runner that
// does not keeps waiting for the turn, since a line typed into it is lost.
func (d *Daemon) escalatesByTyping(taskID string, waitTurn bool, at time.Time) bool {
	return waitTurn && d.act.midTurn(taskID) && d.midTurnInputFor(taskID) && d.heldAged(taskID, at)
}

// turnHoldsAged is turnHolds for a message with an age.
func (d *Daemon) turnHoldsAged(taskID string, waitTurn bool, at time.Time) bool {
	return d.turnHolds(taskID, waitTurn) && !d.escalatesByTyping(taskID, waitTurn, at)
}

// escalationLine is the line in front of an escalated message.
func escalationLine(age time.Duration) string {
	return fmt.Sprintf("[atrium] this message waited %d minutes for your turn to end, so it is delivered now. "+
		"finish the step you are on, then read it.", int(age.Minutes()))
}

// escalatedBody is what the typist types for an escalated message: the same line
// the hook puts in front of one, then the text. A space and not a newline joins
// them, because without bracketed paste a newline would submit the line early.
func (d *Daemon) escalatedBody(taskID string, e pendingMsg) string {
	text := escalationLine(time.Since(e.at)) + " " + e.text
	if d.bracketedPasteFor(taskID, false) {
		return "\x1b[200~" + text + "\x1b[201~"
	}
	return text
}

// noteEscalated records an escalated message on the card and on the say that sent it.
func (d *Daemon) noteEscalated(taskID, msgID, from string, age time.Duration) {
	what := fmt.Sprintf("held message escalated after %dm", int(age.Minutes()))
	if err := d.st.AppendEvent(taskID, store.EventNotified, map[string]any{
		"by": EscalatedBy, "text": what, "from_peer": from, "message": msgID,
	}); err != nil {
		log.Printf("[atrium] could not record an escalated message on %s: %v", taskID, err)
	}
	if err := d.st.NoteSayEscalated(msgID, what); err != nil {
		log.Printf("[atrium] could not note an escalated message on its say: %v", err)
	}
}
