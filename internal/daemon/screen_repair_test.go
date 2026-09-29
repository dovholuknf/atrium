package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// The repair report (item 74, option 3). It measures and never splices, so every
// test here that reads history also reads a grid that was not watching.

// nlines is "line 04" to "line 10" style rows, joined by CR LF with no trailing
// one, which is how a full screen is left when its cursor rests on the last row.
func nlines(from, to int) string {
	rows := make([]string, 0, to-from+1)
	for i := from; i <= to; i++ {
		rows = append(rows, fmt.Sprintf("line %02d", i))
	}
	return strings.Join(rows, "\r\n")
}

// repaint is `ESC[H` and then rows, each ending in erase and CR LF. `last` is
// whether the final row keeps its CR LF, which conhost's does.
func repaint(rows []string, last bool) string {
	var b strings.Builder
	b.WriteString("\x1b[H")
	for i, r := range rows {
		b.WriteString(r + "\x1b[K")
		if i < len(rows)-1 || last {
			b.WriteString("\r\n")
		}
	}
	return b.String()
}

func lineRows(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("line %02d", i))
	}
	return out
}

func newRows(from, to int) []string {
	var out []string
	for i := from; i <= to; i++ {
		out = append(out, fmt.Sprintf("new %02d", i))
	}
	return out
}

func blankRows(n int) []string { return make([]string, n) }

// watched replays b through a watching grid and through one that is not, and
// insists the two agree on everything a rendering could show.
func watched(t *testing.T, cols, rows int, b string, cuts []sizeCut) []repairEvent {
	t.Helper()
	w := newScreenSized(cols, rows)
	w.rep = &repairWatch{}
	w.applyCuts([]byte(b), cuts)
	w.rep.finish()

	c := newScreenSized(cols, rows)
	c.applyCuts([]byte(b), cuts)
	if w.text() != c.text() || len(w.history) != len(c.history) || w.textWithCursor() != c.textWithCursor() {
		t.Fatalf("the detector changed the grid:\nwatched %q\ncontrol %q", w.textWithCursor(), c.textWithCursor())
	}
	return w.rep.events
}

func only(t *testing.T, ev []repairEvent) repairEvent {
	t.Helper()
	if len(ev) != 1 {
		t.Fatalf("want one candidate, got %+v", ev)
	}
	return ev[0]
}

func none(t *testing.T, ev []repairEvent) {
	t.Helper()
	for _, e := range ev {
		if e.outcome == "repaired" {
			t.Fatalf("a false positive was reported as repaired: %+v", e)
		}
	}
}

// THE CASE: a repaint that starts from old row 4 means rows 1 to 3 were
// overwritten in place.
func TestARepaintFromALaterRowIsReportedAsRepairable(t *testing.T) {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	ev := only(t, watched(t, 30, 10, nlines(1, 10)+repaint(rows, true), nil))
	if ev.outcome != "repaired" || ev.k != 3 || ev.added != 3 || ev.cut != 0 || ev.rows != 10 {
		t.Fatalf("got %+v", ev)
	}
	if want := len(nlines(1, 10)); ev.offset != want {
		t.Fatalf("offset %d, want the byte of the ESC[H at %d", ev.offset, want)
	}
}

func TestTheSameWhenTheLastRowHasNoLineFeed(t *testing.T) {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	b := nlines(1, 10) + repaint(rows, false) + "\x1b[2;3H"
	ev := only(t, watched(t, 30, 10, b, nil))
	if ev.outcome != "repaired" || ev.k != 3 || ev.added != 3 {
		t.Fatalf("got %+v", ev)
	}
}

func TestARepaintIdenticalToTheScreenIsNothing(t *testing.T) {
	ev := only(t, watched(t, 30, 10, nlines(1, 10)+repaint(lineRows(1, 10), true), nil))
	if ev.outcome != "no-match" || ev.added != 0 {
		t.Fatalf("got %+v", ev)
	}
}

// Blank rows and a border line up at every k. The distinct rows are there to stop
// exactly this.
func TestMostlyBlankRowsAndOneBorderDoNotMatch(t *testing.T) {
	screen := "------\r\n" + strings.Repeat("\r\n", 8) + "------"
	rows := append(blankRows(9), "------")
	ev := only(t, watched(t, 30, 10, screen+repaint(rows, true), nil))
	if ev.outcome != "no-match" || ev.why != "distinct" {
		t.Fatalf("got %+v", ev)
	}
}

// Two shifts passing is a guess, and the rule does not guess.
func TestTwoPassingShiftsAreAmbiguous(t *testing.T) {
	pat := []string{"AA", "BB", "CC", "DD"}
	var screen []string
	for i := 0; i < 14; i++ {
		screen = append(screen, pat[i%4])
	}
	rows := append(append([]string{}, screen[4:]...), newRows(1, 4)...)
	ev := only(t, watched(t, 30, 14, strings.Join(screen, "\r\n")+repaint(rows, true), nil))
	if ev.outcome != "ambiguous" || ev.k != 0 || ev.added != 0 || ev.why != "k=4,8" {
		t.Fatalf("got %+v", ev)
	}
}

func TestASecondCutInsideARepaintCancelsIt(t *testing.T) {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	head := nlines(1, 10)
	full := repaint(rows, true)
	mid := len(head) + len("\x1b[H") + 3*len("line 04\x1b[K\r\n")
	ev := only(t, watched(t, 30, 10, head+full, []sizeCut{{mid, 25, 0}}))
	if ev.outcome != "cancelled" || ev.why != "cut" {
		t.Fatalf("got %+v", ev)
	}
}

func TestAnythingElseInsideARepaintCancelsIt(t *testing.T) {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	for name, inject := range map[string]string{
		"erase-display": "\x1b[J", "insert-delete": "\x1b[L", "scroll": "\x1b[S",
		"decstbm": "\x1b[2;9r", "alt-screen": "\x1b[?1049h", "cursor-move": "\x1b[5;5H",
	} {
		b := nlines(1, 10) + repaint(rows[:3], true) + inject + strings.Join(rows[3:], "\r\n")
		ev := watched(t, 30, 10, b, nil)
		none(t, ev)
		if len(ev) == 0 || ev[0].outcome != "cancelled" || ev[0].why != name {
			t.Fatalf("%s: got %+v", name, ev)
		}
	}
}

func TestUnderDECSTBMThereIsNoCandidate(t *testing.T) {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	ev := watched(t, 30, 10, nlines(1, 10)+"\x1b[2;9r"+repaint(rows, true), nil)
	if len(ev) != 0 {
		t.Fatalf("got %+v", ev)
	}
}

func TestOnTheAlternateScreenThereIsNoCandidate(t *testing.T) {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	ev := watched(t, 30, 10, nlines(1, 10)+"\x1b[?1049h"+repaint(rows, true), nil)
	if len(ev) != 0 {
		t.Fatalf("got %+v", ev)
	}
}

func TestTheFirstScreenfulIsNotACandidate(t *testing.T) {
	ev := watched(t, 30, 10, nlines(1, 4)+repaint(append(lineRows(2, 4), newRows(5, 11)...), true), nil)
	if len(ev) != 0 {
		t.Fatalf("got %+v", ev)
	}
}

func TestARepaintTheRingEndedInsideIsIncomplete(t *testing.T) {
	ev := only(t, watched(t, 30, 10, nlines(1, 10)+repaint(lineRows(4, 8), true), nil))
	if ev.outcome != "incomplete" {
		t.Fatalf("got %+v", ev)
	}
}

// A KNOWN FALSE POSITIVE, asserted so nobody mistakes it for a bug. Content that
// really moved up because a block above it was removed looks exactly like rows
// that were overwritten, and the rule cannot tell them apart when the rows match.
// It only ever ADDS a line to history, and the report exists to count these.
func TestAReallyCollapsedBlockIsReportedAsRepairable(t *testing.T) {
	rows := append(lineRows(4, 10), blankRows(3)...)
	ev := only(t, watched(t, 30, 10, nlines(1, 10)+repaint(rows, true), nil))
	if ev.outcome != "repaired" || ev.k != 3 || ev.added != 3 {
		t.Fatalf("got %+v", ev)
	}
}

// AT A CUT. 50 rows become 40, so the grid files 10 off the top, and conhost's
// repaint follows at the new height.
func TestARepaintAtAShrinkOnlyAddsWhatTheResizeDidNotFile(t *testing.T) {
	head := nlines(1, 50)
	cut := []sizeCut{{len(head), 30, 40}}

	shifted12 := append(lineRows(13, 50), newRows(51, 52)...)
	ev := only(t, watched(t, 30, 50, head+repaint(shifted12, true), cut))
	if ev.outcome != "repaired" || ev.k != 12 || ev.added != 2 || ev.cut != 50 || ev.rows != 40 {
		t.Fatalf("shifted 12: %+v", ev)
	}

	ev = only(t, watched(t, 30, 50, head+repaint(lineRows(11, 50), true), cut))
	if ev.outcome != "resize-filed" || ev.k != 0 || ev.added != 0 || ev.cut != 50 || ev.why != "k=10 gone=10" {
		t.Fatalf("shifted 10: %+v", ev)
	}
}

func TestARepaintAtAGrowAddsNothing(t *testing.T) {
	head := nlines(1, 40)
	ev := only(t, watched(t, 30, 40, head+repaint(append(lineRows(1, 40), blankRows(10)...), true),
		[]sizeCut{{len(head), 30, 50}}))
	if ev.outcome != "no-match" || ev.added != 0 || ev.cut != 40 {
		t.Fatalf("got %+v", ev)
	}
}

// A repaint that comes well after the cut is not at it.
func TestARepaintAfterSomethingWasDrawnIsNotAtTheCut(t *testing.T) {
	head := nlines(1, 50)
	b := head + "\r\nmore" + repaint(append(lineRows(14, 50), newRows(51, 53)...), true)
	ev := only(t, watched(t, 30, 50, b, []sizeCut{{len(head), 30, 40}}))
	if ev.cut != 0 {
		t.Fatalf("got %+v", ev)
	}
}

func TestTheReportFormat(t *testing.T) {
	got := formatRepairReport([]repairEvent{
		{offset: 9, rows: 10, cols: 30, outcome: "repaired", k: 3, added: 3},
		{offset: 99, rows: 40, cols: 30, cut: 50, outcome: "resize-filed", why: "k=10 gone=10"},
	})
	want := "offset\trows\tcols\tcut\toutcome\tk\tadded\twhy\n" +
		"9\t10\t30\t-\trepaired\t3\t3\t\n" +
		"99\t40\t30\t50\tresize-filed\t0\t0\tk=10 gone=10\n" +
		"totals\trepaired=1\tresize-filed=1\tno-match=0\tambiguous=0\tcancelled=0\tincomplete=0\tadded=3\n"
	if got != want {
		t.Fatalf("got\n%q\nwant\n%q", got, want)
	}
}

// --- through HTTP ---

func repairGet(t *testing.T, d *Daemon, taskID, query string) string {
	t.Helper()
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/v1/tasks/"+taskID+"/scrollback/text?"+query, nil)
	req.SetPathValue("id", taskID)
	d.handleTextScrollback(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("answered %d: %s", w.Code, w.Body.String())
	}
	return w.Body.String()
}

func repairStream() string {
	rows := append(lineRows(4, 10), newRows(11, 13)...)
	return nlines(1, 10) + repaint(rows, true)
}

func TestTheReportThroughHTTP(t *testing.T) {
	d := testDaemon(t)
	_, r := sizedSession(t, d, "rep-card", 30, 10)
	r.buf.Write([]byte(repairStream()))

	got := repairGet(t, d, "rep-card", "repair=report&mode=raw&ansi=1&collapse=0")
	lines := strings.Split(strings.TrimRight(got, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header, one row and totals, got %q", got)
	}
	if lines[0] != "offset\trows\tcols\tcut\toutcome\tk\tadded\twhy" {
		t.Fatalf("header %q", lines[0])
	}
	if want := fmt.Sprintf("%d\t10\t30\t-\trepaired\t3\t3\t", len(nlines(1, 10))); lines[1] != want {
		t.Fatalf("row %q, want %q", lines[1], want)
	}
	if want := "totals\trepaired=1\tresize-filed=0\tno-match=0\tambiguous=0\tcancelled=0\tincomplete=0\tadded=3"; lines[2] != want {
		t.Fatalf("totals %q", lines[2])
	}
	if strings.Contains(got, "[atrium]") {
		t.Fatalf("the report carries a banner: %q", got)
	}
}

func TestTheReportReadsAShellWhenAsked(t *testing.T) {
	d := testDaemon(t)
	_, _ = sizedSession(t, d, "rep-shell", 30, 10)
	sh := &runner{
		taskID: "rep-shell", pty: newFakePTY(), started: time.Now(),
		buf: newRingSized(1<<16, 30, 10), watchers: map[chan []byte]struct{}{}, done: make(chan struct{}),
	}
	sh.buf.Write([]byte(repairStream()))
	d.sup.addShell(sh)

	if got := repairGet(t, d, "rep-shell", "repair=report"); strings.Contains(got, "\trepaired\t") {
		t.Fatalf("the card's own ring is empty and reported %q", got)
	}
	if got := repairGet(t, d, "rep-shell", "repair=report&kind=shell"); !strings.Contains(got, "\trepaired\t3\t3\t") {
		t.Fatalf("the shell's ring was not reported: %q", got)
	}
}

// NOTHING ELSE MOVES. Plain /scrollback/text and the screen replay an attach uses
// are what they were, before and after a report has run over the same bytes.
func TestThePlainReplayIsUnchangedByTheDetector(t *testing.T) {
	d := testDaemon(t)
	_, r := sizedSession(t, d, "rep-plain", 30, 10)
	r.buf.Write([]byte(repairStream()))

	b, cuts, rows, _ := r.buf.ReplayCuts()
	before := string(replayCut(b, "screen", cuts, rows))
	plainBefore := repairGet(t, d, "rep-plain", "mode=screen")

	_ = repairGet(t, d, "rep-plain", "repair=report")
	repairReport(b, cuts, rows)

	if after := string(replayCut(b, "screen", cuts, rows)); after != before {
		t.Fatalf("attach replay changed:\n%q\n%q", before, after)
	}
	if plainAfter := repairGet(t, d, "rep-plain", "mode=screen"); plainAfter != plainBefore {
		t.Fatalf("/scrollback/text changed:\n%q\n%q", plainBefore, plainAfter)
	}
	if !strings.HasPrefix(plainBefore, "[atrium] screen mode, 30 columns, 10 rows,") {
		t.Fatalf("the banner went: %q", plainBefore)
	}
	// And the rows the repaint overwrote are still not in it: nothing is spliced.
	for _, gone := range []string{"line 01", "line 02", "line 03"} {
		if strings.Contains(plainBefore, gone) {
			t.Fatalf("%q is in history, which nothing here may do", gone)
		}
	}
}
