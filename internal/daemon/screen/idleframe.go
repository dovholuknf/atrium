package screen

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

// FrameTailBytes is how much of the ring is rendered to find the last screen.
// Enough to hold several full redraws, so the render starts from a complete one.
const FrameTailBytes = 64 << 10

// Why a frame was judged as it was, for the log line. Told apart so a wrong
// badge can be traced to the classifier and not to hook ordering.
const (
	FrameIdle    = "idle_prompt"
	FrameNoBox   = "no_input_box"
	FrameOpenBox = "box_not_closed"
	FrameHint    = "interrupt_hint"
	FrameSpinner = "spinner_line"
)

// ClassifyFrame reports whether the screen `tail` (raw pty bytes, composed at
// cols by rows) ends with Claude Code at an idle prompt, and the reason either way.
func ClassifyFrame(tail []byte, cols, rows int) (bool, string) {
	sc := NewScreenSized(cols, rows)
	sc.Apply(tail)
	// The live grid only. History is rows that scrolled or were cleared away,
	// and an old working frame there must not veto the one on screen.
	return ClassifyScreen(FrameText(sc))
}

// FramePromptEmpty is whether the screen `tail` shows Claude Code at an idle
// prompt with nothing typed on the input line.
func FramePromptEmpty(tail []byte, cols, rows int) bool {
	sc := NewScreenSized(cols, rows)
	sc.Apply(tail)
	idle, _, typed := classifyScreenPrompt(FrameText(sc))
	return idle && typed == ""
}

// FrameLineEmpty is a looser read than framePromptEmpty, for a runner whose line is only unsure because of a restart.
// It asks for the prompt row under a rule and nothing typed on it, and no interrupt hint in the footer rows. Reply
// text that happens to hold an ellipsis and a bracket, a status line taller than the box classifier allows, and the
// dim suggestion Claude draws in an empty prompt all leave classifyScreenPrompt unsure, and none of them is a draft.
func FrameLineEmpty(tail []byte, cols, rows int) bool {
	sc := NewScreenSized(cols, rows)
	sc.Apply(tail)
	type row struct {
		text    string
		restDim bool
	}
	var lines []row
	for _, cells := range sc.cells {
		var b strings.Builder
		after, dim := false, true
		for _, c := range cells {
			if c.ch == contCh {
				continue
			}
			ch := c.ch
			if ch == 0 {
				ch = ' '
			}
			if after && ch != ' ' && !sgrIsDim(c.sgr) {
				dim = false
			}
			if ch == []rune(framePrompt)[0] {
				after = true
			}
			b.WriteRune(ch)
		}
		if t := strings.TrimSpace(b.String()); t != "" {
			lines = append(lines, row{t, dim})
		}
	}
	n := len(lines)
	for i := n - 1; i >= 0 && i >= n-6; i-- {
		if strings.Contains(lines[i].text, frameInterrupt) {
			return false
		}
	}
	for i := n - 1; i >= 1 && i >= n-9; i-- {
		if !strings.HasPrefix(lines[i].text, framePrompt) || !IsRule(lines[i-1].text) {
			continue
		}
		return strings.TrimSpace(strings.TrimPrefix(lines[i].text, framePrompt)) == "" || lines[i].restDim
	}
	return false
}

// sgrIsDim is whether a cell's SGR sequence draws faint or bright black, the way a placeholder is drawn.
func sgrIsDim(sgr string) bool {
	s := strings.TrimSuffix(strings.TrimPrefix(sgr, "\x1b["), "m")
	if s == sgr || s == "" {
		return false
	}
	toks := strings.Split(s, ";")
	for i := 0; i < len(toks); i++ {
		switch toks[i] {
		case "2", "90":
			return true
		case "38", "48":
			if i+1 < len(toks) && toks[i+1] == "5" {
				i += 2
			} else {
				i += 4
			}
		}
	}
	return false
}

// FrameHasInterrupt is whether any of the last rows of the screen carries the hint a working runner draws.
func FrameHasInterrupt(tail []byte, cols, rows int) bool {
	sc := NewScreenSized(cols, rows)
	sc.Apply(tail)
	var lines []string
	for _, ln := range strings.Split(FrameText(sc), "\n") {
		if ln = strings.TrimSpace(ln); ln != "" {
			lines = append(lines, ln)
		}
	}
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-12; i-- {
		if strings.Contains(lines[i], frameInterrupt) {
			return true
		}
	}
	return false
}

// FrameText is the live grid as text, one line per row. The second cell of a wide
// character holds contCh and is skipped, as writeRow does, so the character
// appears once.
func FrameText(sc *Screen) string {
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

func IsRule(ln string) bool {
	return strings.Count(ln, frameRuleRune) >= frameRuleMin
}

// ClassifyScreen judges rendered screen text, colour already removed.
func ClassifyScreen(text string) (bool, string) {
	idle, why, _ := classifyScreenPrompt(text)
	return idle, why
}

// classifyScreenPrompt is classifyScreen and also the text on the prompt row,
// past the prompt mark, which is "" for an empty input line.
func classifyScreenPrompt(text string) (bool, string, string) {
	var lines []string
	for _, ln := range strings.Split(text, "\n") {
		if ln = strings.TrimRight(ln, " \r\t"); ln != "" {
			lines = append(lines, ln)
		}
	}
	n := len(lines)
	if n < 4 {
		return false, FrameNoBox, ""
	}
	// A hint or a spinner ANYWHERE in the last rows says working. This is wider
	// than where they are drawn on purpose: a screen rebuilt from a 64KB tail can
	// leave a row where it was not, and a misplaced working row must still veto.
	// The silence the caller requires is the other guard, since a running turn
	// redraws its spinner every second.
	for i := n - 1; i >= 0 && i >= n-12; i-- {
		if strings.Contains(lines[i], frameInterrupt) {
			return false, FrameHint, ""
		}
		if strings.Contains(lines[i], "…") && strings.Contains(lines[i], "(") {
			return false, FrameSpinner, ""
		}
	}
	// The bottom rule is the last rule on screen, with the footer under it. A
	// custom status line makes that footer several rows, so allow up to
	// frameFooterRows of them, none of which the veto above found working.
	bottom := -1
	for i := n - 2; i >= 0 && i >= n-1-frameFooterRows; i-- {
		if IsRule(lines[i]) {
			bottom = i
			break
		}
	}
	if bottom < 0 {
		return false, FrameNoBox, ""
	}
	// Up from the bottom rule to the top one: the prompt, which may wrap.
	top := -1
	for i := bottom - 1; i >= 0 && i >= bottom-8; i-- {
		if IsRule(lines[i]) {
			top = i
			break
		}
	}
	if top < 0 {
		return false, FrameOpenBox, ""
	}
	if top+1 >= bottom || !strings.HasPrefix(strings.TrimSpace(lines[top+1]), framePrompt) {
		return false, FrameNoBox, ""
	}
	return true, FrameIdle, strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[top+1]), framePrompt))
}
