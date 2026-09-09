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
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			i = skipEscape(b, i, &out)
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
			out = append(out, '\r', '\n')
		case c == 0x08:
			// Backspace, which a terminal uses to walk back over what it just
			// wrote. Dropped: on a replay there is nothing to walk back over
			// that anybody wants removed.
			i++
		default:
			out = append(out, c)
			i++
		}
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
func skipEscape(b []byte, i int, out *[]byte) int {
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
