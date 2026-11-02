package screen

import (
	"fmt"
	"strconv"
	"strings"
)

// Finding the rows a no-scroll repaint overwrote. See `docs/backlog-2.md` item 74,
// "Option 3, the replay-only repair, designed".
//
// conhost answers a change in the pty's height with a bare `ESC[H` and then every
// row of the screen, each ending `ESC[K CR LF`. When the first row of that repaint
// is a LATER row than the screen held, the rows above it were the ones conhost
// filed into its own history, which it never sends, and the grid here overwrites
// them in place. They never reach `history`.
//
// The other way round, `unfiled`: when the first row is an EARLIER row, held in
// the tail of history, conhost pulled rows back out of its history (a resize that
// unwrapped lines, a taller pty), and the grid here would file them a second time.
//
// The report only measures: `outcome=repaired` means "would be repaired", and
// `added` is the rows that would be spliced. With `splice` set, as `ReplayCut`
// sets it, `close` makes the repair: rows above a later first row are filed, and
// rows of an earlier first row are taken back out of history.
//
// OFF UNLESS `screen.rep` IS SET, and only `RepairReport` and `ReplayCut` set it.
// Every other user of `screen` (idleframe, the live models) carries a nil pointer
// and pays a nil check.

// repairMaxSpan is how many bytes a candidate may consume before it is dropped. A
// repaint is about 50 rows of a few hundred bytes, so this is generous, and it
// stops a `ESC[H` that is followed by a very long stream of plain text from being
// held for the rest of the ring.
const repairMaxSpan = 64 << 10

// RepairEvent is one candidate and how it came out.
type RepairEvent struct {
	offset int // the byte of the ESC[H in the replayed bytes
	rows   int // the grid's size at the candidate
	cols   int
	cut    int // the rows before a height cut the candidate sits at, or 0
	// outcome is repaired, resize-filed, no-match, ambiguous, cancelled or
	// incomplete.
	outcome string
	k       int // the shift, 0 when nothing would be spliced
	added   int // the rows that would be spliced into history
	why     string
}

// RepairWatch is the detector's state. One per report, never shared.
type RepairWatch struct {
	events []RepairEvent
	// pos is the byte `applyCuts` is stepping, so an event can say where it was.
	pos int
	// splice is whether `close` repairs history, not only reports.
	splice bool

	// A row cut, remembered until something is drawn. A candidate opened while it
	// is pending is AT A CUT: it is compared with the grid as it was BEFORE the cut,
	// because that is what the repaint's rows were counted from.
	cutPending bool
	cutSnap    []string
	cutCells   [][]cell
	cutHist    int
	cutRows    int
	cutGone    int // rows `fitRows` filed off the top, all cuts since the last draw
	histBefore int

	open    bool
	openAt  int
	rows    int
	cols    int
	atCut   bool
	preRows int
	gone    int
	snap    []string
	cells   [][]cell // the snapshot's rows, kept only to splice
	hist    int      // history's length the snapshot was taken at
	tail    []string // history's last rows before the snapshot
	lines   []string // the repaint's rows, recorded at each line feed
}

// rowText is a row as text for comparison: no colour, no trailing blanks.
//
// TEXT, NOT SGR. conhost re-renders its buffer, so a row's colours can come back
// as different bytes for the same look.
func rowText(r []cell) string {
	end := len(r)
	for end > 0 && (r[end-1].ch == ' ' || r[end-1].ch == 0) {
		end--
	}
	if end == 0 {
		return ""
	}
	var b strings.Builder
	for _, c := range r[:end] {
		switch {
		case c.ch == contCh:
		case c.ch == 0:
			b.WriteByte(' ')
		default:
			b.WriteRune(c.ch)
		}
	}
	return b.String()
}

func cloneRows(cells [][]cell) [][]cell {
	out := make([][]cell, len(cells))
	for i, r := range cells {
		out[i] = append([]cell(nil), r...)
	}
	return out
}

func snapshotRows(cells [][]cell) []string {
	out := make([]string, len(cells))
	for i, r := range cells {
		out[i] = rowText(r)
	}
	return out
}

// full is the "not the first screenful" rule: something is in history, or the
// bottom row holds text. A grid that has never been full has nothing a repaint
// could have started from a later row of.
func fullSnapshot(snap []string, hist int) bool {
	return hist > 0 || (len(snap) > 0 && snap[len(snap)-1] != "")
}

func (r *RepairWatch) add(ev RepairEvent) { r.events = append(r.events, ev) }

func (r *RepairWatch) cancel(why string) {
	if !r.open {
		return
	}
	r.open = false
	ev := RepairEvent{offset: r.openAt, rows: r.rows, cols: r.cols, outcome: "cancelled", why: why}
	if r.atCut {
		ev.cut = r.preRows
	}
	r.add(ev)
}

// finish reports a candidate the ring ended inside.
func (r *RepairWatch) finish() {
	if !r.open {
		return
	}
	r.open = false
	ev := RepairEvent{offset: r.openAt, rows: r.rows, cols: r.cols, outcome: "incomplete",
		why: fmt.Sprintf("%d of %d rows", len(r.lines), r.rows)}
	if r.atCut {
		ev.cut = r.preRows
	}
	r.add(ev)
}

// shifts is every k from 1 to rows-1 at which the repaint's first M rows equal the
// snapshot's rows k+1 to k+M, with at least four of them non-blank and distinct.
// `thin` is whether any k matched on text and failed only that last rule.
func (r *RepairWatch) shifts() (ks []int, thin bool) {
	m := MaxInt(6, r.rows/4)
	if m > len(r.lines) {
		return nil, false
	}
	for k := 1; k <= r.rows-1 && k+m <= len(r.snap); k++ {
		same := true
		for i := 0; i < m; i++ {
			if r.lines[i] != r.snap[k+i] {
				same = false
				break
			}
		}
		if !same {
			continue
		}
		seen := map[string]bool{}
		for i := 0; i < m; i++ {
			if r.lines[i] != "" {
				seen[r.lines[i]] = true
			}
		}
		if len(seen) < 4 {
			thin = true
			continue
		}
		ks = append(ks, k)
	}
	return ks, thin
}

// ups is every j from 1 to rows-1 at which the repaint's first M rows equal the
// last j rows of history followed by the snapshot, by the same rules as shifts.
// None when the repaint starts where the snapshot does.
func (r *RepairWatch) ups() []int {
	m := MaxInt(6, r.rows/4)
	if m > len(r.lines) || m > len(r.snap) {
		return nil
	}
	at := func(j, i int) string {
		if i < j {
			return r.tail[len(r.tail)-j+i]
		}
		return r.snap[i-j]
	}
	same := func(j int) bool {
		for i := 0; i < m; i++ {
			if r.lines[i] != at(j, i) {
				return false
			}
		}
		return true
	}
	if same(0) {
		return nil
	}
	var js []int
	for j := 1; j <= r.rows-1 && j <= len(r.tail); j++ {
		if !same(j) {
			continue
		}
		seen := map[string]bool{}
		for i := 0; i < m; i++ {
			if r.lines[i] != "" {
				seen[r.lines[i]] = true
			}
		}
		if len(seen) >= 4 {
			js = append(js, j)
		}
	}
	return js
}

// close judges a repaint that is whole. Exactly one passing k or nothing, and
// with `splice` set, makes the repair.
func (r *RepairWatch) close(s *Screen) {
	r.open = false
	ev := RepairEvent{offset: r.openAt, rows: r.rows, cols: r.cols}
	if r.atCut {
		ev.cut = r.preRows
	}
	ks, thin := r.shifts()
	var js []int
	if len(ks) == 0 {
		js = r.ups()
	}
	switch {
	case len(js) == 1:
		ev.outcome, ev.k, ev.why = "unfiled", -js[0], fmt.Sprintf("removed=%d", js[0]+r.gone)
	case len(js) > 1:
		parts := make([]string, len(js))
		for i, j := range js {
			parts[i] = strconv.Itoa(-j)
		}
		ev.outcome, ev.why = "ambiguous", "k="+strings.Join(parts, ",")
	case len(ks) == 0:
		ev.outcome, ev.why = "no-match", "no-k"
		if thin {
			ev.why = "distinct"
		}
	case len(ks) > 1:
		parts := make([]string, len(ks))
		for i, k := range ks {
			parts[i] = strconv.Itoa(k)
		}
		ev.outcome, ev.why = "ambiguous", "k="+strings.Join(parts, ",")
	case r.atCut && ks[0] <= r.gone:
		ev.outcome, ev.why = "resize-filed", fmt.Sprintf("k=%d gone=%d", ks[0], r.gone)
	default:
		ev.outcome, ev.k, ev.added = "repaired", ks[0], ks[0]
		if r.atCut {
			ev.added = ks[0] - r.gone
		}
	}
	r.add(ev)
	if r.splice {
		r.repair(s, ev)
	}
}

// repair files snapshot rows gone to k-1 right after the rows `fitRows` filed, or
// takes the j rows an earlier repaint repeats, and the rows `fitRows` filed, back
// out of history. `r.gone` is 0 away from a cut.
func (r *RepairWatch) repair(s *Screen, ev RepairEvent) {
	at := r.hist + r.gone
	if at > len(s.history) {
		return
	}
	switch ev.outcome {
	case "repaired":
		add := make([][]cell, 0, ev.added)
		for _, row := range r.cells[r.gone:ev.k] {
			add = append(add, append([]cell(nil), row...))
		}
		s.history = append(s.history[:at], append(add, s.history[at:]...)...)
	case "unfiled":
		s.history = append(s.history[:r.hist+ev.k], s.history[at:]...)
	}
}

// --- the hooks on screen. Every one is called only when s.rep is not nil. ---

// repCutStart is called before a size cut is applied.
func (s *Screen) repCutStart(c SizeCut) {
	r := s.rep
	r.cancel("cut")
	r.histBefore = len(s.history)
	if c.Rows > 0 && c.Rows != s.rows && !s.alt {
		if !r.cutPending {
			r.cutSnap = snapshotRows(s.cells)
			if r.splice {
				r.cutCells = cloneRows(s.cells)
			}
			r.cutHist = len(s.history)
			r.cutRows = s.rows
			r.cutGone = 0
		}
		r.cutPending = true
	}
}

// repCutEnd is called after it, and notes what `fitRows` filed off the top.
func (s *Screen) repCutEnd() {
	r := s.rep
	if r.cutPending {
		r.cutGone += len(s.history) - r.histBefore
	}
}

// repPut is a printable character. Something was drawn, so a cut before it is no
// longer what a following `ESC[H` sits at.
func (s *Screen) repPut() { s.rep.cutPending = false }

// repLF is a line feed from the stream, which records the row just finished.
func (s *Screen) repLF() {
	r := s.rep
	r.cutPending = false
	if !r.open {
		return
	}
	r.lines = append(r.lines, s.rowNow())
	if len(r.lines) == r.rows {
		r.close(s)
	}
}

func (s *Screen) rowNow() string {
	if s.row < 0 || s.row >= len(s.cells) {
		return ""
	}
	return rowText(s.cells[s.row])
}

// repMove is a cursor move. It CLOSES an open candidate that has exactly rows-1
// rows (the repaint whose last row has no line feed), and cancels one that has any
// other count. Then, if it is a bare home, it may open a new one.
func (s *Screen) repMove(home bool) {
	r := s.rep
	if r.open {
		if len(r.lines) == r.rows-1 {
			r.lines = append(r.lines, s.rowNow())
			r.close(s)
		} else {
			r.cancel("cursor-move")
		}
	}
	if !home || r.open {
		return
	}
	if s.alt || s.regSet || !s.fixedRows {
		return
	}
	var snap []string
	var cells [][]cell
	var hist int
	if r.cutPending {
		snap, cells, hist = r.cutSnap, r.cutCells, r.cutHist
	} else {
		snap, hist = snapshotRows(s.cells), len(s.history)
		if r.splice {
			cells = cloneRows(s.cells)
		}
	}
	if !fullSnapshot(snap, hist) {
		return
	}
	r.open, r.openAt = true, r.pos
	r.rows, r.cols = s.rows, s.cols
	r.snap, r.cells, r.hist = snap, cells, hist
	r.tail = snapshotRows(s.history[hist-min(hist, s.rows) : hist])
	r.lines = r.lines[:0]
	r.atCut, r.preRows, r.gone = r.cutPending, 0, 0
	if r.atCut {
		r.preRows, r.gone = r.cutRows, r.cutGone
	}
}

// repCSI is every CSI sequence that is not private, before it is applied.
func (s *Screen) repCSI(final byte, home bool) {
	r := s.rep
	switch final {
	case 'm', 'K':
		// Colour and erase-to-end are what a repaint is made of.
	case 'H', 'f':
		s.repMove(home)
	case 'A', 'B', 'C', 'D', 'E', 'F', 'G', '`', 'd', 'u':
		s.repMove(false)
	case 'J':
		r.cancel("erase-display")
	case 'L', 'M':
		r.cancel("insert-delete")
	case 'S', 'T':
		r.cancel("scroll")
	case 'r':
		r.cancel("decstbm")
	case 'P', '@', 'X', 's':
		r.cancel("edit")
	}
}

// RepairReport replays `b` through a grid that is watching, and answers what it
// found. It is `ReplayCut`'s screen arm, minus the rendering, so the bytes and the
// cuts are the ones an attach would have replayed.
func RepairReport(b []byte, cuts []SizeCut, rows int) []RepairEvent {
	cols := 0
	if len(cuts) > 0 {
		cols = cuts[0].Cols
		if cuts[0].Rows > 0 {
			rows = cuts[0].Rows
		}
		cuts = cuts[1:]
	}
	sc := NewScreenSized(cols, rows)
	sc.rep = &RepairWatch{}
	sc.ApplyCuts(b, cuts)
	sc.rep.finish()
	return sc.rep.events
}

var repairOutcomes = []string{"repaired", "unfiled", "resize-filed", "no-match", "ambiguous", "cancelled", "incomplete"}

// FormatRepairReport is tab separated: one header line, one line per candidate,
// and a `totals` line.
func FormatRepairReport(events []RepairEvent) string {
	var b strings.Builder
	b.WriteString("offset\trows\tcols\tcut\toutcome\tk\tadded\twhy\n")
	counts := map[string]int{}
	added := 0
	for _, e := range events {
		cut := "-"
		if e.cut > 0 {
			cut = strconv.Itoa(e.cut)
		}
		fmt.Fprintf(&b, "%d\t%d\t%d\t%s\t%s\t%d\t%d\t%s\n",
			e.offset, e.rows, e.cols, cut, e.outcome, e.k, e.added, e.why)
		counts[e.outcome]++
		added += e.added
	}
	b.WriteString("totals")
	for _, o := range repairOutcomes {
		fmt.Fprintf(&b, "\t%s=%d", o, counts[o])
	}
	fmt.Fprintf(&b, "\tadded=%d\n", added)
	return b.String()
}
