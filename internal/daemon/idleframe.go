package daemon

import (
	"strings"
)

// What Claude Code's last screen frame says about whether it is working.
//
// Claude Code draws a spinner line directly above its input box while a turn is
// running, and the line ends "esc to interrupt". At an idle prompt the box is
// there and the spinner is not. That is the whole signature, and the strings are
// Claude Code's own, so they live here and nowhere else, pinned by tests.
//
// It fails toward NOT flagging. No box, a box drawn half way, or an interrupt
// hint anywhere in the last frame all read as working, because a missed badge
// costs what the board costs today and a false one claims a session is idle
// while it works. See docs/backlog-2.md item 21.
const (
	frameBoxTop    = "╭"
	frameBoxBottom = "╰"
	frameInterrupt = "to interrupt"
)

// frameTailBytes is how much of the ring is read to find the last frame. A frame
// is a few hundred bytes of text, and escapes roughly double that.
const frameTailBytes = 8 << 10

// Why a frame was judged as it was, for the log line. Told apart so a wrong
// badge can be traced to the classifier and not to hook ordering.
const (
	frameIdle    = "idle_prompt"
	frameNoBox   = "no_input_box"
	frameOpenBox = "box_not_closed"
	frameHint    = "interrupt_hint"
)

// classifyFrame reports whether the last frame in `tail` (raw pty bytes) is
// Claude Code at an idle prompt, and the reason either way.
func classifyFrame(tail []byte) (bool, string) {
	text := ansi.ReplaceAllString(string(tail), "")
	top := strings.LastIndex(text, frameBoxTop)
	if top < 0 {
		return false, frameNoBox
	}
	if !strings.Contains(text[top:], frameBoxBottom) {
		return false, frameOpenBox
	}
	// The box and its footer.
	if strings.Contains(text[top:], frameInterrupt) {
		return false, frameHint
	}
	// The spinner sits directly above the box: the last non-empty line before it.
	before := text[:top]
	lines := strings.Split(before, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		ln := strings.TrimSpace(lines[i])
		if ln == "" {
			continue
		}
		if strings.Contains(ln, frameInterrupt) {
			return false, frameHint
		}
		break
	}
	return true, frameIdle
}
