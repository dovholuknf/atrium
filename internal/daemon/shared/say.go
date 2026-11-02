package shared

import (
	"fmt"
	"path/filepath"
	"strings"
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

// ParseWhen reads a `when` a caller sent. Empty is the default, immediate.
func ParseWhen(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", WhenImmediate:
		return WhenImmediate, nil
	case WhenDone:
		return WhenDone, nil
	}
	return "", fmt.Errorf("when is %q or %q, not %q", WhenImmediate, WhenDone, s)
}

const (
	// PeerSendsPerMinute bounds how often one session may message others.
	//
	// A model in a loop is the failure this exists for: without a limit, one
	// confused session can fill every other session's queue faster than any of
	// them drains it. Twenty a minute is far more than deliberate use and far
	// less than a loop.
	PeerSendsPerMinute = 20
)

// QueuedNote is what a sender needs to know next: this did not land now, and
// the first of three things to happen delivers it.
const QueuedNote = "queued. it is typed in when that session's terminal is free, or arrives on its " +
	"next tool call or at the end of its turn, whichever comes first."

// TurnQueuedNote is QueuedNote for a message that waits for the turn to end.
const TurnQueuedNote = "queued until that session's turn ends, then typed in when its terminal is " +
	"free, or carried by the Stop hook."

// WhenWord is what a sender is told its message will wait for.
func WhenWord(waitTurn bool) string {
	if waitTurn {
		return WhenDone
	}
	return WhenImmediate
}

// TypedNote is what a sender is told when its words went straight in.
const TypedNote = "typed into the terminal and sent."

// InsideRoot reports whether path is below root, not root itself.
func InsideRoot(root, path string) bool {
	rel, err := filepath.Rel(root, filepath.FromSlash(path))
	return err == nil && rel != "." && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
