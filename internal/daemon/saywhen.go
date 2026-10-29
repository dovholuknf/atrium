package daemon

import (
	"fmt"
	"strings"

	"github.com/dovholuknf/atrium/internal/runnerprofile"
	"github.com/dovholuknf/atrium/internal/store"
)

// When a message is typed in: IMMEDIATELY by default, or once the turn ends.
//
// Immediate means the gate every automated write goes through and nothing
// else: an empty input line, a quiet keyboard, and no dialog on screen. It is
// the restart wake's gate without the wake's turn check. A runner that reads a
// line typed mid-turn as it arrives (codex) is typed into, so a "stop now" lands
// while the worker is still working, which is what a person typing does.
//
// A RUNNER THAT QUEUES A LINE TYPED MID-TURN (claude: its own queued-message box,
// read at the next step boundary) is never typed into mid-turn by anything but
// the operator's keystrokes. Its hooks carry the message and the answer is
// `queued`. See carriedByHooks and `runnerprofile.Profile.QueuesMidTurn`.
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
	if t == nil || (t.PeerTyping && d.sup.Get(t.ID) != nil) || t.StopHookSeenAt != nil {
		return ""
	}
	return fmt.Sprintf("queued until %s's turn ends, but it has never been heard from on its Stop "+
		"hook, so nothing carries it then. send it immediately instead, or wire the Stop hook "+
		"under rooms > runners.", t.WireName)
}

// TypedQueued is the answer for a line that was typed into a runner mid-turn and has not been read: it is in the
// runner's queued-message box. `terminal` is kept for a line typed into an idle runner, whose prompt submit is its
// read. Only a forced say can type mid-turn into a runner that queues, since nothing else does.
const TypedQueued = "typed-queued"

const typedQueuedNote = "typed into that session's queued-message box mid-turn, and not read yet. it reads the " +
	"box at its next step, which for a long tool call is when the call returns."

// queuesMidTurnFor is the runner setting that says a line typed into this card mid-turn lands in the runner's own
// queued-message box and is not read yet. An unknown runner or a store error answers no: the old behaviour, typed.
func (d *Daemon) queuesMidTurnFor(taskID string) bool {
	h, _ := d.harnessOf(taskID)
	return h != nil && h.QueuesMidTurn
}

// harnessOf is the harness row a card's runner names, with the card.
func (d *Daemon) harnessOf(taskID string) (*store.Harness, *store.Task) {
	t, err := d.st.Get(taskID)
	if err != nil || t == nil || t.Runner == "" {
		return nil, t
	}
	h, err := d.st.Harness(t.Runner)
	if err != nil {
		return nil, t
	}
	return h, t
}

// typedWouldQueue reports whether a line typed into this card right now would only land in the runner's queued-message
// box: the runner queues a mid-turn line and it is mid-turn. Typing is not delivering then, so nothing types for it.
func (d *Daemon) typedWouldQueue(taskID string) bool {
	return d.act.midTurn(taskID) && d.queuesMidTurnFor(taskID)
}

// carriedByHooks reports whether an immediate message is left for its hooks and not typed: the card is a runner that
// queues a line typed mid-turn, it is mid-turn, and its hooks can carry the message. Whoever sent it: a session, the
// operator's own channel, a script.
//
// CLAUDE CODE PUTS A LINE TYPED MID-TURN IN ITS OWN QUEUED-MESSAGE BOX and reads it at the next step boundary, which
// for a worker inside one long tool call is when that call returns (measured, docs/terminal/queued-box-findings.md).
// A `terminal` answer for such a say was untrue. Queued instead, the PreToolUse hook carries it at the next tool call
// (step 2 of the permission chain, see takeMessages) and the Stop hook at the turn's end. Nothing is typed, so the
// injector holds no retry for it and the answer is `queued` until a hook has handed it to the model.
//
// HOOK-CAPABLE, NOT HEARD. A runner whose profile has a hook target is one atrium writes hooks for, so a fresh card
// counts without waiting to be heard. A runner with no target counts only once a tool or stop hook has been heard.
// A card whose hooks were never installed holds the message and the sender is warned: see `reachability`.
func (d *Daemon) carriedByHooks(taskID string, waitTurn bool) bool {
	if waitTurn || !d.act.midTurn(taskID) {
		return false
	}
	h, t := d.harnessOf(taskID)
	if h == nil || !h.QueuesMidTurn {
		return false
	}
	return runnerprofile.For(h).Hooks != "" || t.ToolHookSeenAt != nil || t.StopHookSeenAt != nil
}

// turnHolds reports whether a message that waits for the turn is still held by
// it: it asked to wait and the runner is mid-turn. An immediate message is
// never held by the turn, only by the gate.
func (d *Daemon) turnHolds(taskID string, waitTurn bool) bool {
	return waitTurn && d.act.midTurn(taskID)
}
