package daemon

import (
	"strings"
)

// What Claude Code's last screen says about whether it is working.
//
// Claude Code draws a prompt (`❯`) between two full-width rules, and under it a
// footer such as `⏵⏵ auto mode on (shift+tab to cycle) · esc to interrupt`. The
// words "esc to interrupt" are in the footer only while a turn runs. Above the top
// rule sits a spinner line while it works (`· Ionizing… (57s · ↓ 1.7k tokens)`),
// which at idle becomes a summary (`✻ Crunched for 1m 0s`). The strings are
// Claude Code's own, so they live here and nowhere else, pinned by tests against
// frames captured from a real session (testdata/frame-*.bin).
//
// The frame is read off a RENDERED screen, not off escape-stripped bytes: Claude
// Code redraws with cursor addressing, so stripped bytes interleave old and new
// frames and read as both working and idle at once.
//
// It fails toward NOT flagging. No prompt box, a footer that is not the last
// thing on the screen, an interrupt hint or a spinner anywhere near the box all
// read as working, because a missed badge costs what the board costs today and a
// false one claims a session is idle while it works. See docs/backlog-2.md item 21.
const (
	frameInterrupt = "interrupt"
	framePrompt    = "❯"
	frameRuleRune  = "─"
	// frameRuleMin is how many rule runes make a line a rule. The real ones span
	// the terminal, so this is far under any width Claude Code draws at.
	frameRuleMin = 20
	// frameFooterRows is how many rows may sit under the bottom rule: the hint
	// line, and a custom status line's rows above it.
	frameFooterRows = 4
)

// frameTailBytes is how much of the ring is rendered to find the last screen.
// Enough to hold several full redraws, so the render starts from a complete one.
const frameTailBytes = 64 << 10

// Why a frame was judged as it was, for the log line. Told apart so a wrong
// badge can be traced to the classifier and not to hook ordering.
const (
	frameIdle    = "idle_prompt"
	frameNoBox   = "no_input_box"
	frameOpenBox = "box_not_closed"
	frameHint    = "interrupt_hint"
	frameSpinner = "spinner_line"
)

// classifyFrame reports whether the screen `tail` (raw pty bytes, composed at
// cols by rows) ends with Claude Code at an idle prompt, and the reason either way.
func classifyFrame(tail []byte, cols, rows int) (bool, string) {
	sc := newScreenSized(cols, rows)
	sc.apply(tail)
	// The live grid only. History is rows that scrolled or were cleared away,
	// and an old working frame there must not veto the one on screen.
	return classifyScreen(frameText(sc))
}

// frameText is the live grid as text, one line per row. The second cell of a wide
// character holds contCh and is skipped, as writeRow does, so the character
// appears once.
func frameText(sc *screen) string {
	var b strings.Builder
	for _, r := range sc.cells {
		for _, c := range r {
			if c.ch == contCh {
				continue
			}
			if c.ch == 0 {
				c.ch = ' '
			}
			b.WriteRune(c.ch)
		}
		b.WriteByte('\n')
	}
	return b.String()
}

func isRule(ln string) bool {
	return strings.Count(ln, frameRuleRune) >= frameRuleMin
}

// classifyScreen judges rendered screen text, colour already removed.
func classifyScreen(text string) (bool, string) {
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimRight(ln, " \r\t"); ln != "" {
			lines = append(lines, ln)
		}
	}
	n := len(lines)
	if n < 4 {
		return false, frameNoBox
	}
	// A hint or a spinner ANYWHERE in the last rows says working. This is wider
	// than where they are drawn on purpose: a screen rebuilt from a 64KB tail can
	// leave a row where it was not, and a misplaced working row must still veto. Claude Code uses
	// neither, and the silence the caller requires is the other guard, since a
	// running turn redraws its spinner every second.
	for i := n - 1; i >= 0 && i >= n-12; i-- {
		if strings.Contains(lines[i], frameInterrupt) {
			return false, frameHint
		}
		if strings.Contains(lines[i], "…") && strings.Contains(lines[i], "(") {
			return false, frameSpinner
		}
	}
	// The bottom rule is the last rule on screen, with the footer under it. A
	// custom status line makes that footer several rows, so allow up to
	// frameFooterRows of them, none of which the veto above found working.
	bottom := -1
	for i := n - 2; i >= 0 && i >= n-1-frameFooterRows; i-- {
		if isRule(lines[i]) {
			bottom = i
			break
		}
	}
	if bottom < 0 {
		return false, frameNoBox
	}
	// Up from the bottom rule to the top one: the prompt, which may wrap.
	top := -1
	for i := bottom - 1; i >= 0 && i >= bottom-8; i-- {
		if isRule(lines[i]) {
			top = i
			break
		}
	}
	if top < 0 {
		return false, frameOpenBox
	}
	if top+1 >= bottom || !strings.HasPrefix(strings.TrimSpace(lines[top+1]), framePrompt) {
		return false, frameNoBox
	}
	return true, frameIdle
}
