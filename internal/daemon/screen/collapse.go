package screen

// CollapseRedraws keeps the last frame of repeated in-place updates.
//
// ONLY EVER FOR THE FLATTENER, and running it anywhere else destroys output.
// It has no grid, so it decides what a frame is from `\r` and the erases, and
// `\r` is not only a redraw: claude-code ends every WRAPPED VISUAL ROW with
// one. A paragraph three rows wide is one newline-free segment with three
// boundaries in it, this reads them as three frames, and two rows of the
// paragraph are thrown away before any renderer sees them.
//
// Measured on one card's ring: twelve findings went in, and what came out of
// here had zero of their `#:`, `Sev:` and `Location:` lines and eight of their
// twelve `Issue:` lines. A whole day was spent on renderers that were being
// handed that.
//
// The flattener needs it, because without a grid a spinner that redrew four
// hundred times becomes four hundred lines. The screen model does not: a grid
// resolves a repaint by definition, which is what a grid is for. Same ring
// through the screen model uncollapsed keeps all twelve.
//
// Run during replay to keep parsing off the write path. Never collapse across
// a newline, since completed lines belong in history.
// Detect frames by boundaries, since counters and timers change the text.
// Require two boundaries so a single prompt overwrite is preserved.
func CollapseRedraws(b []byte) []byte {
	out := make([]byte, 0, len(b))
	// seg is the start of the newline free segment being judged, last and prev
	// the starts of the two most recent boundary groups in it, and drawn
	// whether a frame has been written since the last of them.
	seg, last, prev, groups, drawn := 0, -1, -1, 0, false
	flush := func(end int) {
		from := seg
		if groups >= 2 {
			// A trailing boundary with nothing after it is the start of a frame
			// the runner never drew, so the last frame begins one group back.
			if drawn && last > seg {
				from = last
			} else if !drawn && prev > seg {
				from = prev
			}
		}
		out = append(out, b[from:end]...)
	}
	for i := 0; i < len(b); {
		if b[i] == '\n' {
			flush(i + 1)
			seg, last, prev, groups, drawn = i+1, -1, -1, 0, false
			i++
			continue
		}
		if n := redrawAt(b, i); n > 0 {
			// Boundaries with no frame between them are one reset, such as a
			// carriage return followed by an erase to end of line.
			if last < 0 || drawn {
				prev, last, groups, drawn = last, i, groups+1, false
			}
			i += n
			continue
		}
		drawn = true
		i++
	}
	flush(len(b))
	return out
}

// redrawAt returns the length of a redraw boundary at b[i], or zero.
// Recognize carriage return, line/display erases, and synchronized-output
// start (CSI ? 2026 h), which Claude Code uses to delimit frames.
// Absolute cursor positions are not boundaries: a frame can contain several,
// and splitting on each would discard parts of that frame.
func redrawAt(b []byte, i int) int {
	if b[i] == '\r' {
		return 1
	}
	if b[i] != 0x1b || i+1 >= len(b) || b[i+1] != '[' {
		return 0
	}
	j := i + 2
	for j < len(b) && (b[j] >= '0' && b[j] <= '9' || b[j] == ';' || b[j] == '?') {
		j++
	}
	if j >= len(b) {
		return 0
	}
	if b[j] == 'K' || b[j] == 'J' {
		return j + 1 - i
	}
	// The begin marker only. The end marker closes a frame that has already
	// been counted, and treating both as boundaries would count every frame
	// twice, which changes where the two-boundary threshold falls.
	if b[j] == 'h' && string(b[i+2:j]) == "?2026" {
		return j + 1 - i
	}
	return 0
}

// StripSGR removes the colour that `flatten` deliberately keeps.
//
// Two callers want the same bytes and disagree about exactly one thing. A
// terminal wants the colour, because a transcript in one colour is much harder
// to read. A browser tab cannot render it and shows the codes as text.
func StripSGR(b []byte) []byte {
	out := make([]byte, 0, len(b))
	for i := 0; i < len(b); {
		if b[i] != 0x1b {
			out = append(out, b[i])
			i++
			continue
		}
		// Only SGR can be here, since flatten dropped everything else, but the
		// scan is written to consume any CSI so a stray sequence cannot leave
		// its parameters on screen.
		j := i + 1
		if j < len(b) && b[j] == '[' {
			j++
			for j < len(b) && b[j] >= 0x30 && b[j] <= 0x3f {
				j++
			}
			for j < len(b) && b[j] >= 0x20 && b[j] <= 0x2f {
				j++
			}
			if j < len(b) {
				j++
			}
			i = j
			continue
		}
		i = j + 1
	}
	return out
}
