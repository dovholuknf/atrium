package daemon

import (
	"strconv"
	"strings"

	"github.com/dovholuknf/atrium/internal/store"
)

// Replay output through a screen grid to preserve partial row updates,
// spinners, and redrawn prompts. Stripping cursor movement alone leaves
// fragments because updates depend on text already on screen.
//
// Rows scrolled out of the grid become history; the final grid alone would
// preserve only the last screenful. This handles sequences used by supported
// runners, not a complete terminal emulator. See apply.

// screenRows is the starting height when the stream provides none.
// The ring records columns but not rows, so grow the grid as larger rows are
// addressed. Start small: scrolling a row into history early preserves it,
// while an oversized grid can let later repaints overwrite older content.
const screenRows = 24

// screenMaxRows bounds that growth. A runner addressing row 10,000 is a
// corrupt parameter, not a very tall window, and a grid that believed it would
// allocate a hundred megabytes of blank cells.
const screenMaxRows = 400

// cell holds a character and its SGR sequence. Preserve colour sequences
// verbatim because replay does not need to interpret individual attributes.
type cell struct {
	ch  rune
	sgr string
}

var blank = cell{ch: ' '}

// screen is a grid, a cursor, and the lines that have scrolled off it.
type screen struct {
	cols, rows int
	cells      [][]cell
	row, col   int
	sgr        string
	// wrapNext defers wrapping until the next character. A carriage return or
	// cursor move cancels it. Wrapping immediately after the last column would
	// add a blank line after full-width output.
	wrapNext bool
	// history is everything that has scrolled off the top, oldest first.
	history [][]cell
	// attr is the SGR state `sgr` is rendered from. Held apart from the
	// string because an attribute is set and cleared independently of the
	// others, and a string can only be appended to.
	attr sgrState
	// saved is DECSC, the cursor position an application stashes before
	// drawing something and restores after.
	savedRow, savedCol int
	savedSGR           sgrState
	// fixedRows is whether `rows` came from a record of the real terminal or
	// from this file's own guess. A real height is a ceiling: the grid does
	// not grow past it, because a terminal does not either. A guess has to be
	// allowed to grow, since guessing short and refusing to grow would stack
	// distinct rows on top of each other.
	fixedRows bool
	// alt is the alternate screen buffer. A full-screen program switches to it,
	// draws, and switches back, and none of what it drew is history: that is
	// the whole point of the buffer. What it drew is dropped.
	alt      bool
	altCells [][]cell
	altRow   int
	altCol   int
}

func newScreen(cols int) *screen { return newScreenSized(cols, 0) }

// newScreenSized builds the grid at the size the output was drawn at.
//
// THE HEIGHT IS THE PART THAT WAS BEING GUESSED, and it decides what becomes
// history rather than how anything wraps. A grid taller than the terminal that
// wrote these bytes keeps rows the session had already scrolled past, and the
// next repaint lands on top of them: content that a shorter grid would have
// filed away is silently overwritten. That is why replaying through a screen
// preserved LESS of a real session than not replaying through one at all.
//
// Zero means nothing recorded a height, which is every buffer written before
// the ring carried one. `screenRows` stands in, and `grow` still stretches the
// grid to any row that gets addressed, so an under-estimate costs nothing and
// an over-estimate costs history.
func newScreenSized(cols, rows int) *screen {
	if cols <= 0 {
		cols = 80
	}
	fixed := rows > 0
	if rows <= 0 {
		rows = screenRows
	}
	if rows > screenMaxRows {
		rows = screenMaxRows
	}
	s := &screen{cols: cols, rows: rows, fixedRows: fixed}
	s.cells = make([][]cell, s.rows)
	for i := range s.cells {
		s.cells[i] = blankRow(cols)
	}
	return s
}

func blankRow(cols int) []cell {
	r := make([]cell, cols)
	for i := range r {
		r[i] = blank
	}
	return r
}

// grow expands the grid to hold an addressed row.
//
// ONLY WHEN THE HEIGHT IS A GUESS. This was unconditional, on the reasoning
// that the original height was unknown and clamping to an estimate would merge
// rows that were distinct. The ring records the height now, and where it does,
// growing is the opposite of what a terminal does and it costs the whole
// feature.
//
// A terminal of thirty rows addressed at row forty-five does not become
// forty-five rows tall. It scrolls, and the rows that go off the top are
// history and can never be written on again. A grid that grows instead keeps
// those rows addressable, so the next repaint lands on top of them and they
// are gone. Measured on a real session: the expanded file listings claude
// prints and then collapses to `+31 lines (ctrl+o to expand)` were all lost
// this way, 154 lines of a 361 line capture, because they never scrolled.
func (s *screen) grow(toRow int) {
	if toRow < s.rows {
		return
	}
	// A recorded height is the terminal's real height. Anything past the
	// bottom row is clamped to it, the same as a terminal clamps it.
	if s.fixedRows {
		return
	}
	if toRow >= screenMaxRows {
		toRow = screenMaxRows - 1
	}
	for len(s.cells) <= toRow {
		s.cells = append(s.cells, blankRow(s.cols))
	}
	s.rows = len(s.cells)
}

// scroll moves the grid up one row and saves the top row in history.
func (s *screen) scroll() {
	if s.alt {
		// The alternate screen has no scrollback, by definition. A full-screen
		// program scrolling its own view is not producing history.
		copy(s.cells, s.cells[1:])
		s.cells[len(s.cells)-1] = blankRow(s.cols)
		return
	}
	s.history = append(s.history, s.cells[0])
	copy(s.cells, s.cells[1:])
	s.cells[len(s.cells)-1] = blankRow(s.cols)
}

// put writes one character at the cursor and advances it.
func (s *screen) put(ch rune) {
	if s.wrapNext {
		s.col = 0
		s.lineFeed()
		s.wrapNext = false
	}
	s.grow(s.row)
	if s.row >= len(s.cells) {
		s.row = len(s.cells) - 1
	}
	if s.col >= s.cols {
		s.col = s.cols - 1
	}
	s.cells[s.row][s.col] = cell{ch: ch, sgr: s.sgr}
	if s.col == s.cols-1 {
		// Deferred, not taken. See `wrapNext`.
		s.wrapNext = true
		return
	}
	s.col++
}

func (s *screen) lineFeed() {
	s.wrapNext = false
	if s.row >= s.rows-1 {
		s.scroll()
		s.row = s.rows - 1
		return
	}
	s.row++
}

func (s *screen) moveTo(row, col int) {
	s.wrapNext = false
	if row < 0 {
		row = 0
	}
	if col < 0 {
		col = 0
	}
	if col >= s.cols {
		col = s.cols - 1
	}
	s.grow(row)
	if row >= s.rows {
		row = s.rows - 1
	}
	s.row, s.col = row, col
}

// eraseLine is `CSI K`: 0 to the end, 1 to the start, 2 the whole line.
func (s *screen) eraseLine(mode int) {
	s.grow(s.row)
	r := s.cells[s.row]
	from, to := s.col, s.cols
	switch mode {
	case 1:
		from, to = 0, s.col+1
	case 2:
		from, to = 0, s.cols
	}
	for i := from; i < to && i < len(r); i++ {
		r[i] = blank
	}
}

// eraseDisplay handles CSI J. For full-screen clears (2 and 3), preserve
// existing rows in history before clearing the grid.
func (s *screen) eraseDisplay(mode int) {
	s.grow(s.row)
	switch mode {
	case 0:
		s.eraseLine(0)
		for i := s.row + 1; i < s.rows; i++ {
			s.cells[i] = blankRow(s.cols)
		}
	case 1:
		s.eraseLine(1)
		for i := 0; i < s.row; i++ {
			s.cells[i] = blankRow(s.cols)
		}
	default:
		if !s.alt {
			for i := 0; i < s.rows; i++ {
				if !rowIsBlank(s.cells[i]) {
					s.history = append(s.history, s.cells[i])
				}
			}
		}
		for i := 0; i < s.rows; i++ {
			s.cells[i] = blankRow(s.cols)
		}
	}
}

// insertLines is `CSI L`, and deleteLines is `CSI M`. Both move the lines below
// the cursor, which is how an application opens or closes a gap in a list.
func (s *screen) insertLines(n int) {
	s.grow(s.row)
	for k := 0; k < n; k++ {
		copy(s.cells[s.row+1:], s.cells[s.row:])
		s.cells[s.row] = blankRow(s.cols)
	}
}

func (s *screen) deleteLines(n int) {
	s.grow(s.row)
	for k := 0; k < n; k++ {
		// The line leaving is not history: it is being removed from a view the
		// application is rearranging, and it was never below the fold.
		copy(s.cells[s.row:], s.cells[s.row+1:])
		s.cells[len(s.cells)-1] = blankRow(s.cols)
	}
}

func rowIsBlank(r []cell) bool {
	for _, c := range r {
		if c.ch != ' ' && c.ch != 0 {
			return false
		}
	}
	return true
}

// toAlt and fromAlt switch buffers. What the alternate screen holds is never
// history, so entering it stashes the real screen and leaving it puts it back.
func (s *screen) toAlt() {
	if s.alt {
		return
	}
	s.alt = true
	s.altCells = s.cells
	s.altRow, s.altCol = s.row, s.col
	s.cells = make([][]cell, s.rows)
	for i := range s.cells {
		s.cells[i] = blankRow(s.cols)
	}
	s.row, s.col = 0, 0
}

func (s *screen) fromAlt() {
	if !s.alt {
		return
	}
	s.alt = false
	s.cells = s.altCells
	s.rows = len(s.cells)
	s.row, s.col = s.altRow, s.altCol
	s.altCells = nil
}

// text renders scrolled history followed by the current screen. Trim grid
// padding and trailing blank rows. Emit colour only when it changes and
// reset it at line endings.
func (s *screen) text() string {
	body, _, _, _ := s.render()
	return body
}

// textWithCursor is text() plus a trailing move that leaves the terminal's
// cursor where the SESSION left its own, which is what the replay owes an
// attaching viewer.
//
// The renderer reconstructs every cell and then the terminal sits its cursor at
// the end of the last line it was handed. A terminal user interface does not
// leave its cursor there: it parks it in its input box, some rows up from the
// bottom-most output and at a column it worked out. Without this, the first
// character the operator types echoes at the end of the output instead, and the
// pane reads as corrupt.
//
// The move is RELATIVE, from the resting position at the bottom, because the
// transcript is appended to whatever scrollback the terminal already holds and
// there is no absolute row to aim at. That is safe precisely because a live
// cursor sits within a screenful of the end: it is never up in the history that
// has already scrolled past, so walking it up from the bottom always reaches
// it. See `TestReplayRestoresTheCursor`.
//
// Until 8400fa8 this was done for free by the resize every attach performed:
// the SIGWINCH made the runner repaint, and the repaint carried an absolute
// cursor move. Once an attach at an unchanged size stopped resizing, the repaint
// stopped coming, and restoring the cursor became the replay's own job.
func (s *screen) textWithCursor() string {
	if s.fixedRows && !s.alt {
		return s.textAtRows()
	}
	body, curLine, total, ok := s.render()
	if !ok {
		return body
	}
	var b strings.Builder
	b.WriteString(body)
	// The terminal rests one line below the last emitted row, at column zero.
	// Walk up to the cursor's row and across to its column.
	if up := total - curLine; up > 0 {
		b.WriteString("\x1b[" + strconv.Itoa(up) + "A")
	}
	b.WriteString("\r")
	if s.col > 0 {
		b.WriteString("\x1b[" + strconv.Itoa(s.col) + "C")
	}
	return b.String()
}

// textAtRows is the replay when the grid's height is the terminal's real one:
// the history, then the screen ROW FOR ROW, then the cursor put back with an
// absolute move.
//
// A TUI keeps drawing after the attach, and once its input box grows it draws
// with absolute moves (`CSI row;col H`) into its own screen rows. Those land
// right only when the attaching terminal holds the session's screen at the same
// rows. So the screen is not blank-collapsed, and not trimmed at the bottom, the
// way `render` does to the history: claude leaves two blank rows under its
// banner, collapsing them replayed everything below one row high, and a wrapped
// line of input then drew over the rule under the prompt.
//
// The screen's last row is written without a line ending, so the terminal
// stops on it rather than scrolling one more, and the screen's first row is the
// top of the viewport. That holds whatever the history's length, provided the
// attaching terminal is this grid's height, which the pty size frame sent ahead
// of the replay sees to. See `TestReplayKeepsTheSessionsRows`.
func (s *screen) textAtRows() string {
	var b strings.Builder
	cur := ""
	blanks := 0
	for _, r := range s.history {
		// Blank runs in the history still collapse to one, for the reason
		// `render` gives. Nothing addresses history rows, so nothing moves.
		if rowIsBlank(r) {
			blanks++
			if blanks > 1 {
				continue
			}
		} else {
			blanks = 0
		}
		writeRow(&b, r, &cur)
		b.WriteString("\r\n")
	}
	for i, r := range s.cells {
		writeRow(&b, r, &cur)
		if i < len(s.cells)-1 {
			b.WriteString("\r\n")
		}
	}
	b.WriteString("\x1b[" + strconv.Itoa(s.row+1) + ";" + strconv.Itoa(s.col+1) + "H")
	return b.String()
}

// writeRow writes one row, trailing blanks dropped, with its colour reset at
// the end the way `render` does.
func writeRow(b *strings.Builder, r []cell, cur *string) {
	end := len(r)
	for end > 0 && (r[end-1].ch == ' ' || r[end-1].ch == 0) {
		end--
	}
	for j := 0; j < end; j++ {
		c := r[j]
		if c.sgr != *cur {
			if c.sgr == "" {
				b.WriteString("\x1b[m")
			} else {
				b.WriteString(c.sgr)
			}
			*cur = c.sgr
		}
		ch := c.ch
		if ch == 0 {
			ch = ' '
		}
		b.WriteRune(ch)
	}
	if *cur != "" {
		b.WriteString("\x1b[m")
		*cur = ""
	}
}

// render is text() plus where the cursor ended up: the emitted-line index of
// the row the session's cursor is on, the number of lines emitted, and whether
// the cursor is on emitted content at all. The cursor line is tracked through
// the same trimming and blank-collapse the body goes through, since a naive
// map from grid row to output line is wrong the moment either fires.
//
// `ok` is false when the cursor is below the last emitted row, which is a fresh
// prompt with nothing under it: the resting position is already right and no
// move is owed.
func (s *screen) render() (body string, curLine, total int, ok bool) {
	var b strings.Builder
	rows := append(append([][]cell{}, s.history...), s.cells...)
	// Where the session's cursor sits in the combined rows.
	curRow := len(s.history) + s.row

	// Drop blank lines at the very end. A screen is mostly empty and its
	// padding is not part of what was said.
	last := len(rows) - 1
	for last >= 0 && rowIsBlank(rows[last]) {
		last--
	}

	// RUNS OF BLANK ROWS COLLAPSE TO ONE, which is the single biggest thing
	// between this and what a real terminal shows.
	//
	// Measured on one session: the native terminal's own capture was 13% blank
	// lines and this was 56%, so more than half the pane was empty rows to
	// scroll past. They are not invented. A repaint scrolls the grid, the rows
	// that go off the top are whatever was on them, and a great many of them
	// were blank because the region being repainted is taller than the text in
	// it. Every one is a faithful record of a row that was empty, and a
	// thousand faithful records of nothing is not what the operator is looking
	// for.
	//
	// ONE IS KEPT, because a blank line between two blocks is how the runner
	// separates them and dropping it runs them together. Past that a run says
	// nothing the first one did not.
	//
	// The same rule and the same reasoning as `squeezeBlanks` in `flatten.go`,
	// applied here rather than shared because that one works on bytes and this
	// works on rows, and the row is the thing that knows it is blank without
	// having to parse colour back out of it.
	blanks := 0
	cur := ""
	for i := 0; i <= last; i++ {
		r := rows[i]
		skip := false
		if rowIsBlank(r) {
			blanks++
			if blanks > 1 {
				skip = true
			}
		} else {
			blanks = 0
		}
		// The cursor's row, mapped to the line it is emitted on. A collapsed
		// blank folds onto the one blank line its run kept.
		if i == curRow {
			if skip {
				if total > 0 {
					curLine, ok = total-1, true
				}
			} else {
				curLine, ok = total, true
			}
		}
		if skip {
			continue
		}
		end := len(r)
		for end > 0 && r[end-1].ch == ' ' || (end > 0 && r[end-1].ch == 0) {
			end--
		}
		for j := 0; j < end; j++ {
			c := r[j]
			if c.sgr != cur {
				if c.sgr == "" {
					b.WriteString("\x1b[m")
				} else {
					b.WriteString(c.sgr)
				}
				cur = c.sgr
			}
			ch := c.ch
			if ch == 0 {
				ch = ' '
			}
			b.WriteRune(ch)
		}
		if cur != "" {
			b.WriteString("\x1b[m")
			cur = ""
		}
		b.WriteString("\r\n")
		total++
	}
	return b.String(), curLine, total, ok
}

// apply feeds bytes into the screen model. Skip unsupported sequences
// without changing the grid.
func (s *screen) apply(b []byte) { s.applyCuts(b, nil) }

// widthCut is where replayed output changed width: from byte `at` on, it was
// composed for a terminal `cols` wide.
type widthCut struct{ at, cols int }

// applyCuts is apply with the grid resized at each cut, the way the terminal
// that produced the bytes was.
//
// ONE WIDTH FOR THE WHOLE RING IS WHAT DOUBLED THE LINES. After a room restart
// a resumed session reprints its transcript at the width the card was saved
// at, and the operator's pane is often a few columns narrower. Laid into a grid
// at the newest width, every line padded to the full saved width ran past the
// edge, the padding wrapped onto a row of its own, and the replay showed a
// blank line under every full-width coloured diff line. Resizing at the mark
// keeps each run on the grid it was drawn for, and the history it already
// scrolled keeps the width it had.
//
// A cut that falls inside an escape sequence or a rune is taken at the next
// boundary, so resizing never splits one.
func (s *screen) applyCuts(b []byte, cuts []widthCut) {
	for i := 0; i < len(b); {
		for len(cuts) > 0 && cuts[0].at <= i {
			s.resize(cuts[0].cols)
			cuts = cuts[1:]
		}
		i = s.step(b, i)
	}
	for _, c := range cuts {
		s.resize(c.cols)
	}
}

// step applies the one character or sequence at b[i] and returns where the
// next one starts.
func (s *screen) step(b []byte, i int) int {
	c := b[i]
	switch {
	case c == 0x1b:
		return s.escape(b, i)
	case c == '\n':
		s.lineFeed()
	case c == '\r':
		s.col = 0
		s.wrapNext = false
	case c == '\b':
		if s.col > 0 {
			s.col--
		}
		s.wrapNext = false
	case c == '\t':
		next := (s.col/8 + 1) * 8
		if next >= s.cols {
			next = s.cols - 1
		}
		s.col = next
		s.wrapNext = false
	case c == 0x07:
		// A bell rings, it does not draw.
	case c < 0x20:
		// Any other control character. Nothing on screen, nothing here.
	default:
		r, n := decodeRune(b[i:])
		s.put(r)
		return i + n
	}
	return i + 1
}

// resize changes the grid's width the way a terminal without reflow does: rows
// on the grid are cut or padded, and rows already in history are left alone,
// because they were composed at the width they have.
func (s *screen) resize(cols int) {
	if cols <= 0 || cols == s.cols {
		return
	}
	fit := func(rows [][]cell) {
		for i, r := range rows {
			if len(r) > cols {
				rows[i] = r[:cols:cols]
				continue
			}
			for len(r) < cols {
				r = append(r, blank)
			}
			rows[i] = r
		}
	}
	fit(s.cells)
	fit(s.altCells)
	s.cols = cols
	if s.col >= cols {
		s.col = cols - 1
	}
	if s.altCol >= cols {
		s.altCol = cols - 1
	}
	if s.savedCol >= cols {
		s.savedCol = cols - 1
	}
	s.wrapNext = false
}

// decodeRune reads one UTF-8 character, tolerating a sequence cut in half by
// the ring buffer's own boundary.
func decodeRune(b []byte) (rune, int) {
	if len(b) == 0 {
		return ' ', 1
	}
	if b[0] < 0x80 {
		return rune(b[0]), 1
	}
	r := []rune(string(b[:min(len(b), 4)]))
	if len(r) == 0 {
		return ' ', 1
	}
	return r[0], len(string(r[0]))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// escape consumes one escape sequence and returns the index after it.
func (s *screen) escape(b []byte, i int) int {
	start := i
	i++
	if i >= len(b) {
		return len(b)
	}
	switch b[i] {
	case '[':
		return s.csi(b, start, i+1)
	case ']':
		// OSC: a string ended by BEL or ST. A window title, a hyperlink.
		// Nothing it says is on the grid.
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
		// DCS, SOS, PM, APC. Strings, ended by ST.
		i++
		for i < len(b) {
			if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
				return i + 2
			}
			i++
		}
		return len(b)
	case '7':
		s.savedRow, s.savedCol, s.savedSGR = s.row, s.col, s.attr
		return i + 1
	case '8':
		s.moveTo(s.savedRow, s.savedCol)
		s.attr = s.savedSGR
		s.sgr = s.attr.render()
		return i + 1
	case 'M':
		// Reverse index: up one, scrolling the screen down at the top.
		if s.row == 0 {
			copy(s.cells[1:], s.cells)
			s.cells[0] = blankRow(s.cols)
		} else {
			s.row--
		}
		return i + 1
	case 'c':
		// Full reset. The screen is cleared and what was on it was still seen.
		s.eraseDisplay(2)
		s.moveTo(0, 0)
		s.attr = sgrState{}
		s.sgr = ""
		return i + 1
	default:
		// A two byte escape this does not implement, plus the charset
		// selectors which take one more byte.
		if b[i] == '(' || b[i] == ')' || b[i] == '*' || b[i] == '+' {
			return i + 2
		}
		return i + 1
	}
}

// csi handles `ESC [ ... final`.
func (s *screen) csi(b []byte, start, i int) int {
	priv := false
	from := i
	for i < len(b) && b[i] >= 0x30 && b[i] <= 0x3f {
		if b[i] == '?' {
			priv = true
		}
		i++
	}
	params := string(b[from:i])
	for i < len(b) && b[i] >= 0x20 && b[i] <= 0x2f {
		i++
	}
	if i >= len(b) {
		return len(b)
	}
	final := b[i]
	i++

	if priv {
		s.privateMode(strings.TrimPrefix(params, "?"), final)
		return i
	}
	// THE OTHER PRIVATE MARKERS DRAW NOTHING, and reading them as the standard
	// sequence moves the cursor. Claude pushes and pops the kitty keyboard
	// protocol (`CSI > 5 u`, `CSI < u`) and sets xterm's modifyOtherKeys
	// (`CSI > 4 ; 2 m`) at startup and after a key like ctrl-delete. Taken as
	// plain `u` and `m` they became a cursor restore and dim underline, so the
	// next input redraw landed on whatever row was last saved, the banner by
	// default, and the replay drew the typed text over it. A terminal ignores
	// them, and so does this.
	if params != "" && (params[0] == '<' || params[0] == '=' || params[0] == '>') {
		return i
	}

	n := csiNums(params)
	arg := func(k, def int) int {
		if k < len(n) && n[k] > 0 {
			return n[k]
		}
		return def
	}

	switch final {
	case 'H', 'f':
		s.moveTo(arg(0, 1)-1, arg(1, 1)-1)
	case 'A':
		s.moveTo(maxInt(0, s.row-arg(0, 1)), s.col)
	case 'B':
		s.moveTo(s.row+arg(0, 1), s.col)
	case 'C':
		s.moveTo(s.row, s.col+arg(0, 1))
	case 'D':
		s.moveTo(s.row, maxInt(0, s.col-arg(0, 1)))
	case 'E':
		s.moveTo(s.row+arg(0, 1), 0)
	case 'F':
		s.moveTo(maxInt(0, s.row-arg(0, 1)), 0)
	case 'G', '`':
		s.moveTo(s.row, arg(0, 1)-1)
	case 'd':
		s.moveTo(arg(0, 1)-1, s.col)
	case 'J':
		s.eraseDisplay(arg(0, 0))
	case 'K':
		s.eraseLine(arg(0, 0))
	case 'L':
		s.insertLines(arg(0, 1))
	case 'M':
		s.deleteLines(arg(0, 1))
	case 'P':
		// Delete characters, pulling the rest of the line left.
		s.grow(s.row)
		r := s.cells[s.row]
		k := arg(0, 1)
		if s.col < len(r) {
			copy(r[s.col:], r[minInt(s.col+k, len(r)):])
			for j := maxInt(s.col, len(r)-k); j < len(r); j++ {
				r[j] = blank
			}
		}
	case '@':
		// Insert blanks, pushing the rest of the line right.
		s.grow(s.row)
		r := s.cells[s.row]
		k := arg(0, 1)
		if s.col < len(r) {
			copy(r[minInt(s.col+k, len(r)):], r[s.col:])
			for j := s.col; j < minInt(s.col+k, len(r)); j++ {
				r[j] = blank
			}
		}
	case 'X':
		// Erase characters in place.
		s.grow(s.row)
		r := s.cells[s.row]
		for j := s.col; j < minInt(s.col+arg(0, 1), len(r)); j++ {
			r[j] = blank
		}
	case 'S':
		for k := 0; k < arg(0, 1); k++ {
			s.scroll()
		}
	case 'T':
		for k := 0; k < arg(0, 1); k++ {
			copy(s.cells[1:], s.cells)
			s.cells[0] = blankRow(s.cols)
		}
	case 'm':
		s.setSGR(string(b[start:i-1]) + "m")
	case 's':
		s.savedRow, s.savedCol, s.savedSGR = s.row, s.col, s.attr
	case 'u':
		s.moveTo(s.savedRow, s.savedCol)
		s.attr = s.savedSGR
		s.sgr = s.attr.render()
	}
	return i
}

// sgrState is the attributes in force, as a terminal holds them: ONE value per
// attribute, each replaced when a new sequence sets it.
//
// This replaces a string that sequences were APPENDED to. Appending looks
// right, because a terminal reading `31m` and then `32m` does end up green,
// and it is wrong in the way that matters here: claude-code changes colour
// thousands of times without ever resetting, so a single cell ended up
// carrying `[32m[33m[90m[32m[90m[32m[90m[38;2;255;193;7m` and every run of
// text in the replay re-emitted the whole pile. The old code capped it at 512
// bytes and then threw the lot away, which is a leak with a lid on it.
type sgrState struct {
	bold, dim, italic, under, blink, inverse, hidden, strike bool
	// fg and bg are the parameters as written, so `31`, `38;5;12` and
	// `38;2;120;200;90` all round trip without this needing a colour model.
	// Empty is the terminal's own default.
	fg, bg string
}

// render writes the state as one canonical sequence, or "" when it is default.
//
// ALWAYS FROM A RESET, so a cell does not depend on whatever was in force
// before it. Rows here are reordered by scrolling and written into history out
// of the order they were drawn in, and an attribute that leaks across that
// boundary colours text that had nothing to do with it.
func (a sgrState) render() string {
	if a == (sgrState{}) {
		return ""
	}
	var b strings.Builder
	b.WriteString("\x1b[0")
	for _, f := range []struct {
		on bool
		n  string
	}{
		{a.bold, "1"}, {a.dim, "2"}, {a.italic, "3"}, {a.under, "4"},
		{a.blink, "5"}, {a.inverse, "7"}, {a.hidden, "8"}, {a.strike, "9"},
	} {
		if f.on {
			b.WriteString(";")
			b.WriteString(f.n)
		}
	}
	if a.fg != "" {
		b.WriteString(";")
		b.WriteString(a.fg)
	}
	if a.bg != "" {
		b.WriteString(";")
		b.WriteString(a.bg)
	}
	b.WriteString("m")
	return b.String()
}

// setSGR applies one `ESC [ ... m` to the attribute state.
//
// No parameters means `0`: `ESC [ m` and `ESC [ 0 m` are the same thing and
// both clear everything, colours included.
func (s *screen) setSGR(seq string) {
	params := strings.TrimSuffix(strings.TrimPrefix(seq, "\x1b["), "m")
	if params == "" {
		params = "0"
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		n, err := strconv.Atoi(strings.TrimSpace(parts[i]))
		if err != nil {
			continue
		}
		switch {
		case n == 0:
			s.attr = sgrState{}
		case n == 1:
			s.attr.bold = true
		case n == 2:
			s.attr.dim = true
		case n == 3:
			s.attr.italic = true
		case n == 4:
			s.attr.under = true
		case n == 5:
			s.attr.blink = true
		case n == 7:
			s.attr.inverse = true
		case n == 8:
			s.attr.hidden = true
		case n == 9:
			s.attr.strike = true
		// 21 and 22 both end bold, and 22 ends dim along with it.
		case n == 21 || n == 22:
			s.attr.bold, s.attr.dim = false, false
		case n == 23:
			s.attr.italic = false
		case n == 24:
			s.attr.under = false
		case n == 25:
			s.attr.blink = false
		case n == 27:
			s.attr.inverse = false
		case n == 28:
			s.attr.hidden = false
		case n == 29:
			s.attr.strike = false
		case n >= 30 && n <= 37, n >= 90 && n <= 97:
			s.attr.fg = parts[i]
		case n == 39:
			s.attr.fg = ""
		case n >= 40 && n <= 47, n >= 100 && n <= 107:
			s.attr.bg = parts[i]
		case n == 49:
			s.attr.bg = ""
		// EXTENDED COLOUR EATS ITS OWN PARAMETERS. `38;5;n` is one colour in
		// three parts and `38;2;r;g;b` is one in five, so the loop has to step
		// past them. Reading them as separate attributes is how a green
		// component lands as "set the background to bright black".
		case n == 38 || n == 48:
			taken, text := extendedColour(parts, i)
			if taken == 0 {
				continue
			}
			if n == 38 {
				s.attr.fg = text
			} else {
				s.attr.bg = text
			}
			i += taken - 1
		}
	}
	s.sgr = s.attr.render()
}

// extendedColour reads `38;5;n` or `38;2;r;g;b` starting at parts[i], and
// answers how many parts it used and the text to keep. Zero means the sequence
// was cut short, which the ring can do at its own boundary.
func extendedColour(parts []string, i int) (int, string) {
	if i+1 >= len(parts) {
		return 0, ""
	}
	switch parts[i+1] {
	case "5":
		if i+2 >= len(parts) {
			return 0, ""
		}
		return 3, strings.Join(parts[i:i+3], ";")
	case "2":
		if i+4 >= len(parts) {
			return 0, ""
		}
		return 5, strings.Join(parts[i:i+5], ";")
	}
	return 0, ""
}

// privateMode is `ESC [ ? ... h` or `l`. Only the screen buffer matters here.
func (s *screen) privateMode(params string, final byte) {
	for _, p := range strings.Split(params, ";") {
		switch p {
		case "1049", "47", "1047":
			if final == 'h' {
				s.toAlt()
			} else if final == 'l' {
				s.fromAlt()
			}
		}
	}
}

func csiNums(params string) []int {
	if params == "" {
		return nil
	}
	parts := strings.Split(params, ";")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.Atoi(p)
		if err != nil || v < 0 {
			v = 0
		}
		if v > screenMaxRows*100 {
			v = screenMaxRows * 100
		}
		out = append(out, v)
	}
	return out
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// renderHistory is the entry point: bytes in, transcript out.
func renderHistory(b []byte, cols int) []byte {
	if len(b) == 0 {
		return nil
	}
	s := newScreen(cols)
	s.apply(b)
	return []byte(s.text())
}

// replayMode is how this daemon has been asked to replay history.
//
// Read on every attach rather than cached, so flipping it takes effect on the
// next attach instead of on the next restart. An attach is a websocket upgrade
// and one settings read is nothing beside it.
//
// A STORE THAT CANNOT ANSWER GETS THE DEFAULT. A halted store means the daemon
// is already reporting a failure, and picking some other rendering on top of
// that would be a second surprise.
func replayMode(st *store.Store) string {
	v, err := st.Setting(store.SettingReplayMode)
	if err != nil {
		return "screen"
	}
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "raw":
		return "raw"
	case "flat", "flatten", "on", "1", "true", "yes":
		return "flat"
	}
	return "screen"
}

// replayCols selects the last recorded width to match the newest output.
// Using the attaching browser's width would misplace cursor updates.
func replayCols(widths []int, wantCols int) int {
	if n := len(widths); n > 0 && widths[n-1] > 0 {
		return widths[n-1]
	}
	if wantCols > 0 {
		return wantCols
	}
	return 80
}
