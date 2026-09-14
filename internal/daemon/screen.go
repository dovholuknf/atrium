package daemon

import (
	"strconv"
	"strings"
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
	// saved is DECSC, the cursor position an application stashes before
	// drawing something and restores after.
	savedRow, savedCol int
	savedSGR           string
	// alt is the alternate screen buffer. A full-screen program switches to it,
	// draws, and switches back, and none of what it drew is history: that is
	// the whole point of the buffer. What it drew is dropped.
	alt      bool
	altCells [][]cell
	altRow   int
	altCol   int
}

func newScreen(cols int) *screen {
	if cols <= 0 {
		cols = 80
	}
	s := &screen{cols: cols, rows: screenRows}
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

// grow expands the grid to hold an addressed row. The original height is
// unknown, so clamping to the initial estimate would merge distinct rows.
func (s *screen) grow(toRow int) {
	if toRow < s.rows {
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
	var b strings.Builder
	rows := append(append([][]cell{}, s.history...), s.cells...)

	// Drop blank lines at the very end. A screen is mostly empty and its
	// padding is not part of what was said.
	last := len(rows) - 1
	for last >= 0 && rowIsBlank(rows[last]) {
		last--
	}

	cur := ""
	for i := 0; i <= last; i++ {
		r := rows[i]
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
	}
	return b.String()
}

// apply feeds bytes into the screen model. Skip unsupported sequences
// without changing the grid.
func (s *screen) apply(b []byte) {
	for i := 0; i < len(b); {
		c := b[i]
		switch {
		case c == 0x1b:
			i = s.escape(b, i)
		case c == '\n':
			s.lineFeed()
			i++
		case c == '\r':
			s.col = 0
			s.wrapNext = false
			i++
		case c == '\b':
			if s.col > 0 {
				s.col--
			}
			s.wrapNext = false
			i++
		case c == '\t':
			next := (s.col/8 + 1) * 8
			if next >= s.cols {
				next = s.cols - 1
			}
			s.col = next
			s.wrapNext = false
			i++
		case c == 0x07:
			// A bell rings, it does not draw.
			i++
		case c < 0x20:
			// Any other control character. Nothing on screen, nothing here.
			i++
		default:
			r, n := decodeRune(b[i:])
			s.put(r)
			i += n
		}
	}
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
		s.savedRow, s.savedCol, s.savedSGR = s.row, s.col, s.sgr
		return i + 1
	case '8':
		s.moveTo(s.savedRow, s.savedCol)
		s.sgr = s.savedSGR
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
		s.savedRow, s.savedCol, s.savedSGR = s.row, s.col, s.sgr
	case 'u':
		s.moveTo(s.savedRow, s.savedCol)
		s.sgr = s.savedSGR
	}
	return i
}

// setSGR records colour and style changes. Reset clears accumulated state;
// other sequences append so attributes such as bold and red combine. Bound
// the accumulated string for streams with many changes and no text.
func (s *screen) setSGR(seq string) {
	if seq == "\x1b[m" || seq == "\x1b[0m" || strings.HasPrefix(seq, "\x1b[0;") {
		s.sgr = ""
		if seq == "\x1b[m" || seq == "\x1b[0m" {
			return
		}
	}
	if len(s.sgr) > 512 {
		s.sgr = seq
		return
	}
	s.sgr += seq
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

// replayCols selects the last recorded width to match the newest output.
// Using the attaching browser's width would misplace cursor updates.
// widthNote in attach.go reports a mismatch to the operator.
func replayCols(widths []int, wantCols int) int {
	if n := len(widths); n > 0 && widths[n-1] > 0 {
		return widths[n-1]
	}
	if wantCols > 0 {
		return wantCols
	}
	return 80
}
