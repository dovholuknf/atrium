package daemon

import ()

// Regression cases from captured Claude Code output. Synchronized-output
// markers delimit frames even without carriage returns. Cursor-forward
// sequences must preserve spacing between fields when flattened.

// frame is one spinner frame the way claude-code draws it: hold the screen,
// position absolutely, write, release.
func frame(text string) string {
	return "\x1b[?2026h\x1b[46;3H\x1b[38;2;215;119;87m" + text + "\x1b[m\x1b[?2026l"
}
