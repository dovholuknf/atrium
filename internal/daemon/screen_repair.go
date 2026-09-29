package daemon

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
// THIS FILE ONLY MEASURES. It notices such a repaint and says what a repair WOULD
// add, and it changes nothing: the grid, its history and every rendering are what
// they were without it. `outcome=repaired` means "would be repaired", and `added`
// is the rows that would be spliced. The splice is the one small addition this was
// written to take, at `close`, and it has not been made because clint approved the
// report that says how often it matters and nothing past it.
//
// OFF UNLESS `screen.rep` IS SET, and only `repairReport` sets it. Every other user
// of `screen` (idleframe, attach replay, plain `/scrollback/text`) carries a nil
// pointer and pays a nil check.

// repairMaxSpan is how many bytes a candidate may consume before it is dropped. A
// repaint is about 50 rows of a few hundred bytes, so this is generous, and it
// stops a `ESC[H` that is followed by a very long stream of plain text from being
// held for the rest of the ring.
const repairMaxSpan = 64 << 10

// repairEvent is one candidate and how it came out.
type repairEvent struct {
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

// repairWatch is the detector's state. One per report, never shared.
type repairWatch struct {
	events []repairEvent
	// pos is the byte `applyCuts` is stepping, so an event can say where it was.
	pos int

	// A row cut, remembered until something is drawn. A candidate opened while it
	// is pending is AT A CUT: it is compared with the grid as it was BEFORE the cut,
	// because that is what the repaint's rows were counted from.
	cutPending bool
	cutSnap    []string
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

func (r *repairWatch) add(ev repairEvent) { r.events = append(r.events, ev) }

func (r *repairWatch) cancel(why string) {
	if !r.open {
		return
	}
	r.open = false
	ev := repairEvent{offset: r.openAt, rows: r.rows, cols: r.cols, outcome: "cancelled", why: why}
	if r.atCut {
		ev.cut = r.preRows
	}
	r.add(ev)
}

// finish reports a candidate the ring ended inside.
func (r *repairWatch) finish() {
	if !r.open {
		return
	}
	r.open = false
	ev := repairEvent{offset: r.openAt, rows: r.rows, cols: r.cols, outcome: "incomplete",
		why: fmt.Sprintf("%d of %d rows", len(r.lines), r.rows)}
	if r.atCut {
		ev.cut = r.preRows
	}
	r.add(ev)
}

// shifts is every k from 1 to rows-1 at which the repaint's first M rows equal the
// snapshot's rows k+1 to k+M, with at least four of them non-blank and distinct.
// `thin` is whether any k matched on text and failed only that last rule.
func (r *repairWatch) shifts() (ks []int, thin bool) {
	m := maxInt(6, r.rows/4)
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

// close judges a repaint that is whole. Exactly one passing k or nothing.
//
// The splice belongs here, and is not written. Not at a cut, it would be
// snapshot rows 1 to k at the history length noted on open. At a cut, only rows
// gone+1 to k, at the pre-cut history length plus gone, which is right after the
// rows `fitRows` filed.
func (r *repairWatch) close() {
	r.open = false
	ev := repairEvent{offset: r.openAt, rows: r.rows, cols: r.cols}
	if r.atCut {
		ev.cut = r.preRows
	}
	ks, thin := r.shifts()
	switch {
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
}

// --- the hooks on screen. Every one is called only when s.rep is not nil. ---

// repCutStart is called before a size cut is applied.
func (s *screen) repCutStart(c sizeCut) {
	r := s.rep
	r.cancel("cut")
	r.histBefore = len(s.history)
	if c.rows > 0 && c.rows != s.rows && !s.alt {
		if !r.cutPending {
			r.cutSnap = snapshotRows(s.cells)
			r.cutHist = len(s.history)
			r.cutRows = s.rows
			r.cutGone = 0
		}
		r.cutPending = true
	}
}

// repCutEnd is called after it, and notes what `fitRows` filed off the top.
func (s *screen) repCutEnd() {
	r := s.rep
	if r.cutPending {
		r.cutGone += len(s.history) - r.histBefore
	}
}

// repPut is a printable character. Something was drawn, so a cut before it is no
// longer what a following `ESC[H` sits at.
func (s *screen) repPut() { s.rep.cutPending = false }

// repLF is a line feed from the stream, which records the row just finished.
func (s *screen) repLF() {
	r := s.rep
	r.cutPending = false
	if !r.open {
		return
	}
	r.lines = append(r.lines, s.rowNow())
	if len(r.lines) == r.rows {
		r.close()
	}
}

func (s *screen) rowNow() string {
	if s.row < 0 || s.row >= len(s.cells) {
		return ""
	}
	return rowText(s.cells[s.row])
}

// repMove is a cursor move. It CLOSES an open candidate that has exactly rows-1
// rows (the repaint whose last row has no line feed), and cancels one that has any
// other count. Then, if it is a bare home, it may open a new one.
func (s *screen) repMove(home bool) {
	r := s.rep
	if r.open {
		if len(r.lines) == r.rows-1 {
			r.lines = append(r.lines, s.rowNow())
			r.close()
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
	var hist int
	if r.cutPending {
		snap, hist = r.cutSnap, r.cutHist
	} else {
		snap, hist = snapshotRows(s.cells), len(s.history)
	}
	if !fullSnapshot(snap, hist) {
		return
	}
	r.open, r.openAt = true, r.pos
	r.rows, r.cols = s.rows, s.cols
	r.snap = snap
	r.lines = r.lines[:0]
	r.atCut, r.preRows, r.gone = r.cutPending, 0, 0
	if r.atCut {
		r.preRows, r.gone = r.cutRows, r.cutGone
	}
}

// repCSI is every CSI sequence that is not private, before it is applied.
func (s *screen) repCSI(final byte, home bool) {
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

// repairReport replays `b` through a grid that is watching, and answers what it
// found. It is `replayCut`'s screen arm, minus the rendering, so the bytes and the
// cuts are the ones an attach would have replayed.
func repairReport(b []byte, cuts []sizeCut, rows int) []repairEvent {
	cols := 0
	if len(cuts) > 0 {
		cols = cuts[0].cols
		if cuts[0].rows > 0 {
			rows = cuts[0].rows
		}
		cuts = cuts[1:]
	}
	sc := newScreenSized(cols, rows)
	sc.rep = &repairWatch{}
	sc.applyCuts(b, cuts)
	sc.rep.finish()
	return sc.rep.events
}

var repairOutcomes = []string{"repaired", "resize-filed", "no-match", "ambiguous", "cancelled", "incomplete"}

// formatRepairReport is tab separated: one header line, one line per candidate,
// and a `totals` line.
func formatRepairReport(events []repairEvent) string {
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
