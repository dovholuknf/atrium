package daemon

import (
	"strings"
)

// The screen model, tested against the shapes that broke the flattener.
//
// Every one of these came off an operator's pane, not out of a spec. The
// flattener could not fix any of them because it has no grid, and a grid is the
// only thing that can: a repaint means "put this on top of that", and "that"
// lives on the screen.

// render is the whole pipeline at one width.
func render(in string, cols int) string {
	return string(renderHistory([]byte(in), cols))
}

// lines of output, with the trailing line ending dropped.
func lines(out string) []string {
	out = strings.TrimSuffix(strings.ReplaceAll(out, "\r\n", "\n"), "\n")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// plain strips colour, so an assertion about text is not an assertion about
// how it was painted.
func plain(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			j := i + 1
			if j < len(s) && s[j] == '[' {
				j++
				for j < len(s) && s[j] >= 0x20 && s[j] <= 0x3f {
					j++
				}
				if j < len(s) {
					j++
				}
			}
			i = j
			continue
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// ── the three failures that caused this ─────────────────

// ── what a transcript has to keep ───────────────────────

// ── colour ──────────────────────────────────────────────

// ── the bits that are easy to get wrong ─────────────────
