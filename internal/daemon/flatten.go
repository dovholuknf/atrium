package daemon

import "bytes"

// FLATTENING REPLAYED HISTORY SO IT CANNOT ERASE ITSELF.
//
// Handing over the whole ring buffer was not enough, and the reason took three
// attempts to find. The bytes arrive and then destroy each other.
//
// A terminal user interface does not append. It draws by moving the cursor to
// an absolute row and column, erasing the line it landed on, and writing over
// it. claude-code redraws its own block on every turn that way, and a spinner
// or a progress bar redraws one line hundreds of times. That is correct
// behaviour against a LIVE terminal, where each redraw replaces the thing it
// is a newer version of.
//
// Replay it into a terminal that has just been handed an hour of history and
// every one of those moves lands on the history instead. A megabyte of
// scrollback came down the socket and collapsed into a couple of screens,
// which is indistinguishable from the buffer having been empty. Measured on
// one card: 375 absolute cursor moves and 1,651 erase-in-line sequences in
// 1.6MB of carried output.
//
// So the backlog is flattened before it is sent. Anything that can move the
// cursor off the line it is on, erase, scroll, or switch screen buffers is
// dropped, and every bare carriage return becomes a line ending. What is left
// is the same text in the same order, appended rather than composed, which is
// the form history has to be in to survive being read.
//
// WHAT THIS COSTS, stated because it is a real cost and not a rounding error:
//
//   - A box, a table or a progress bar that redrew in place becomes every
//     version of itself, one after another. More lines than were ever on
//     screen at once, all of them true.
//   - The final rendered state no longer matches what the runner believes is
//     on its screen. The next full redraw puts that right, and a resize or a
//     keystroke provokes one.
//
// Both are paid on HISTORY ONLY. Live output goes through untouched, so a
// terminal user interface works normally from the moment you are attached.
// The trade is deliberate: the alternative on offer was an empty pane.

// flatten returns the bytes with everything that could overwrite what came
// before it removed.
//
// Colour is kept, because a transcript without it is markedly harder to read
// and SGR cannot move or erase anything. Everything else in the escape
// vocabulary is dropped rather than interpreted: this is not a terminal
// emulator and does not need to be one to answer "can this byte destroy the
// line above it".
func flatten(b []byte) []byte {
	if len(b) == 0 {
		return b
	}
	out := make([]byte, 0, len(b)+len(b)/8)
	// Track the cursor row to split position-based output into lines.
	// This flattener does not keep a grid or earlier row contents.
	row := 1
	// Defer a break until text is written. Several cursor moves without output
	// should produce one break, not a blank line for every move.
	pend := owedBreak{}
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			i = skipEscape(b, i, &out, &row, &pend)
		case c == '\r':
			// A RUN OF CARRIAGE RETURNS IS ONE LINE ENDING.
			//
			// A bare `\r` returns to column zero so the next write lands on
			// top of the line already there, which is how a spinner works and
			// is exactly the overwrite being removed. Turning each one into a
			// line ending keeps every version that was drawn.
			//
			// The run is collapsed rather than converted one for one, because
			// `\r\r\n` and `\r\n` both mean one break and expanding them would
			// pad the transcript with blank lines.
			for i < len(b) && b[i] == '\r' {
				i++
			}
			if i < len(b) && b[i] == '\n' {
				i++
			}
			// A real line ending settles anything a cursor move owed: the
			// break has happened, so owing another would double it.
			pend.clear()
			out = append(out, '\r', '\n')
			row++
		case c == '\n':
			pend.clear()
			out = append(out, '\n')
			row++
			i++
		case c == 0x08:
			// Backspace, which a terminal uses to walk back over what it just
			// wrote. Dropped: on a replay there is nothing to walk back over
			// that anybody wants removed.
			i++
		default:
			pend.pay(&out)
			out = append(out, c)
			i++
		}
	}
	return squeezeBlanks(out)
}

// squeezeBlanks reduces consecutive visually empty lines to one.
// Redraws can leave many blank rows in the transcript. Keep one for paragraph
// spacing; whitespace and colour changes alone do not make a line nonblank.
// Apply this after flattening the whole result.
func squeezeBlanks(b []byte) []byte {
	lines := splitKeepingEndings(b)
	out := make([]byte, 0, len(b))
	blanks := 0
	for _, l := range lines {
		if !visuallyEmpty(l) {
			blanks = 0
			out = append(out, l...)
			continue
		}
		blanks++
		// ONE IS KEPT, because a blank line between blocks is how the runner
		// separates them and removing it runs them together. Past that, a run
		// says nothing the first one did not, and it is pages of empty screen
		// to scroll through.
		if blanks == 1 {
			out = append(out, l...)
		}
	}
	return out
}

// visuallyEmpty reports whether a line would show nothing.
//
// Escape sequences and whitespace only. This does not need to know what any
// sequence MEANS, because none of them puts a character on the screen: the
// only sequences that survive flattening are colour, and the only other bytes
// this file emits without a character behind them are spaces.
func visuallyEmpty(line []byte) bool {
	for i := 0; i < len(line); {
		c := line[i]
		if c == 0x1b {
			i++
			if i < len(line) && line[i] == '[' {
				i++
				for i < len(line) && line[i] >= 0x20 && line[i] <= 0x3f {
					i++
				}
				if i < len(line) {
					i++
				}
				continue
			}
			if i < len(line) {
				i++
			}
			continue
		}
		if c != ' ' && c != '\t' && c != '\r' && c != '\n' {
			return false
		}
		i++
	}
	return true
}

// splitKeepingEndings cuts into lines with each line's ending still attached,
// so reassembling is a concatenation and no ending is invented or lost.
func splitKeepingEndings(b []byte) [][]byte {
	var out [][]byte
	start := 0
	for i := 0; i < len(b); i++ {
		if b[i] != '\n' {
			continue
		}
		out = append(out, b[start:i+1])
		start = i + 1
	}
	if start < len(b) {
		out = append(out, b[start:])
	}
	return out
}

// isBracketedPaste says whether a whole CSI sequence is the bracketed paste
// mode being turned on or off.
//
// EXACTLY THOSE TWO AND NOTHING ELSE. It would be shorter to keep every
// private mode that does not draw, and that is how the alternate screen buffer
// gets back in: `ESC [ ? 1049 h` does not draw either, and replaying it takes
// the whole scrollback off the screen at once.
func isBracketedPaste(seq []byte) bool {
	return bytes.Equal(seq, []byte("\x1b[?2004h")) || bytes.Equal(seq, []byte("\x1b[?2004l"))
}

// skipEscape consumes one escape sequence starting at `i`, appending it to
// `out` if it is safe, and returns the index after it.
//
// A sequence cut in half by the ring buffer's own boundary is consumed to the
// end of the input and emitted as nothing, which is the same answer the ring
// gives for a snapshot that starts inside one.
func skipEscape(b []byte, i int, out *[]byte, row *int, pend *owedBreak) int {
	start := i
	i++ // the escape itself
	if i >= len(b) {
		return len(b)
	}
	switch b[i] {
	case '[':
		// CSI: parameters, then intermediates, then one final byte that says
		// what it is.
		i++
		for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
			i++
		}
		for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
			i++
		}
		if i >= len(b) {
			return len(b)
		}
		final := b[i]
		i++
		// SGR is kept. Colour, bold and underline change how the next
		// character looks and cannot move or erase anything.
		//
		// Everything else is dropped by being on the wrong side of this
		// condition rather than by being listed, which matters: the list of
		// ways to move a cursor is long, includes sequences nobody here has
		// heard of, and a private-mode set that switches to the alternate
		// screen buffer would take the entire replay with it.
		//
		// AND BRACKETED PASTE, which is not about drawing at all.
		//
		// `ESC [ ? 2004 h` is how the session says it understands a paste as
		// one thing rather than as fast typing, and the board reads it off
		// this stream to decide whether to wrap a paste in the markers. Every
		// other sequence here is dropped because of what it does to the
		// SCREEN, and this one does nothing to the screen: dropping it changed
		// what happens to the operator's INPUT.
		//
		// The cost of getting that wrong is a paste arriving as five. The
		// pseudo terminal's input pipe holds about four kilobytes and a larger
		// write is delivered in installments, measured at 4096 bytes each with
		// milliseconds between them, so a session that decides "typed or
		// pasted" from arrival timing sees a burst per installment. The
		// markers are what make that timing irrelevant, and they were only
		// sent when this sequence had been seen.
		if final == 'm' || isBracketedPaste(b[start:i]) {
			*out = append(*out, b[start:i]...)
			return i
		}
		// Translate cursor-forward into spaces to preserve gaps between fields.
		// Clamp the parameter so malformed input cannot allocate a huge string.
		params := b[start+2 : i-1]
		if final == 'C' {
			if n := csiParam(params, 1); n > 0 {
				if n > maxSkipColumns {
					n = maxSkipColumns
				}
				// Spacing is writing, so it settles what a move owed. A run of
				// jumps followed by an indent is one break and then the
				// indent, not a blank line per jump.
				pend.pay(out)
				for k := 0; k < n; k++ {
					*out = append(*out, ' ')
				}
			}
			return i
		}
		// Treat movement to a later row as a pending break, with the column as
		// indentation. Earlier rows cannot be rewritten in this append-only output.
		if final == 'H' || final == 'f' {
			want, col := csiRowCol(params)
			if want > *row {
				// Queue one break regardless of jump distance. Skipped screen rows may hold
				// existing content, so reproducing the distance would invent blank lines.
				pend.owe(col)
				*row = want
			} else {
				// Ignore backward movement but keep the following text. Partial repaints
				// can leave fragments because there is no grid to reconstruct the row.
				// Suppressing those fragments also dropped valid content in captures.
				*row = want
			}
		}
		return i
	case ']':
		// OSC: a string, ended by BEL or by ESC \. Window titles and
		// hyperlinks live here. Dropped whole, since a title is not history
		// and an unterminated one would swallow the rest of the buffer.
		i++
		for i < len(b) {
			if b[i] == 0x07 {
				return i + 1
			}
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
			i++
		}
		return len(b)
	case 'P', 'X', '^', '_':
		// DCS, SOS, PM and APC. Also strings, also ended by ST.
		i++
		for i < len(b) {
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
			i++
		}
		return len(b)
	default:
		// A two byte escape: a full reset, a save or restore of the cursor, a
		// reverse index that scrolls the screen. All dropped.
		return i + 1
	}
}

// maxSkipColumns caps spaces produced by cursor-forward sequences to limit
// allocation from malformed or excessively large parameters.
const maxSkipColumns = 1000

// csiParam reads the first numeric CSI parameter, using def when omitted
// or invalid. Ring boundaries can leave incomplete sequences.
func csiParam(params []byte, def int) int {
	if len(params) == 0 {
		return def
	}
	n, seen := 0, false
	for _, c := range params {
		if c == ';' {
			break
		}
		if c < '0' || c > '9' {
			return def
		}
		seen = true
		n = n*10 + int(c-'0')
		if n > maxSkipColumns {
			return maxSkipColumns
		}
	}
	if !seen {
		return def
	}
	return n
}

// csiRowCol reads CSI row and column parameters, defaulting each to one
// when omitted or invalid, including sequences cut by the ring boundary.
func csiRowCol(params []byte) (row, col int) {
	row, col = 1, 1
	if len(params) == 0 {
		return row, col
	}
	// A private-mode sequence is not a position, whatever its final byte.
	if params[0] == '?' {
		return row, col
	}
	i := 0
	for ; i < len(params) && params[i] != ';'; i++ {
	}
	row = csiParam(params[:i], 1)
	if i < len(params) {
		col = csiParam(params[i+1:], 1)
	}
	return row, col
}

// owedBreak emits a pending line ending before the next printable character.
// Multiple cursor moves share one break; trailing moves emit none.
type owedBreak struct {
	owed bool
	// col is where the last move landed, so an indented row comes back
	// indented. The LAST move wins: earlier ones in a run drew nothing, so
	// their columns describe nothing.
	col int
}

// owe records that a break is due before the next thing written.
func (p *owedBreak) owe(col int) {
	p.owed = true
	p.col = col
}

// clear drops the debt, for when a real line ending has already happened.
func (p *owedBreak) clear() {
	p.owed = false
	p.col = 0
}

// pay writes the break and the indent, once, immediately before text.
func (p *owedBreak) pay(out *[]byte) {
	if !p.owed {
		return
	}
	*out = append(*out, '\r', '\n')
	for k := 1; k < p.col && k <= maxSkipColumns; k++ {
		*out = append(*out, ' ')
	}
	p.clear()
}
