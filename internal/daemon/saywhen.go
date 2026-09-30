package daemon

import (
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// When a message is typed in: IMMEDIATELY by default, or once the turn ends.
//
// Immediate means the gate every automated write goes through and nothing
// else: an empty input line, a quiet keyboard, and no dialog on screen. It is
// the restart wake's gate without the wake's turn check. Claude Code queues a
// line typed mid-turn and reads it at its next step, so a "stop now" lands
// while the worker is still working, which is what a person typing does.
//
// Done is the old rule, kept for a message that should not disturb a worker
// mid-thought. It waits for the turn to end as well as for the gate. The hooks
// respect it too: the permission hook leaves a done message for the Stop hook,
// because a message carried by the next tool call is delivered mid-turn by
// another route. See `Message.WaitTurn`.
//
// A RUNNER THAT DOES NOT TAKE INPUT MID-TURN falls back to done for every
// message. That is a setting on its row on the runners page, seeded from
// `runnerprofile`, because it is a fact about the runner and not the sender.
//
// Decided 2026-09-24. It reverses the N7 rule in docs/terminal/typing-race.md, which
// held every peer message for the turn and so let a "stop now" sent to four
// workers reach none of them.
const (
	WhenImmediate = "immediate"
	WhenDone      = "done"
)

// parseWhen reads a `when` a caller sent. Empty is the default, immediate.
func parseWhen(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", WhenImmediate:
		return WhenImmediate, nil
	case WhenDone:
		return WhenDone, nil
	}
	return "", fmt.Errorf("when is %q or %q, not %q", WhenImmediate, WhenDone, s)
}

// waitsForTurn resolves a sender's `when` against the card's runner: true when
// the message has to wait for the turn to end.
func (d *Daemon) waitsForTurn(taskID, when string) bool {
	if when == WhenDone {
		return true
	}
	return !d.midTurnInputFor(taskID)
}

// midTurnInputFor is the runner setting for a card. An unknown runner or a
// store error answers no, which waits for the turn: the slower delivery, and
// never the one that merges a message into a turn that will not read it.
func (d *Daemon) midTurnInputFor(taskID string) bool {
	t, err := d.st.Get(taskID)
	if err != nil || t == nil || t.Runner == "" {
		return false
	}
	h, err := d.st.Harness(t.Runner)
	if err != nil || h == nil {
		return false
	}
	return h.MidTurnInput
}

// turnReachWarning is what to tell the sender of a message that waits for the
// turn when nothing will carry it then, or "" when something will.
//
// A done message is the Stop hook's alone where atrium cannot type it, and the
// Stop hook is optional and off by default. Without it the message waits
// forever while the sender is told `queued`.
func (d *Daemon) turnReachWarning(t *store.Task) string {
	if t == nil || (t.PeerTyping && d.sup.get(t.ID) != nil) || t.StopHookSeenAt != nil {
		return ""
	}
	return fmt.Sprintf("queued until %s's turn ends, but it has never been heard from on its Stop "+
		"hook, so nothing carries it then. send it immediately instead, or wire the Stop hook "+
		"under rooms > runners.", t.WireName)
}

// turnHolds reports whether a message that waits for the turn is still held by
// it: it asked to wait and the runner is mid-turn. An immediate message is
// never held by the turn, only by the gate.
func (d *Daemon) turnHolds(taskID string, waitTurn bool) bool {
	return waitTurn && d.act.midTurn(taskID)
}
