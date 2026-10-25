//go:build integration

package daemon

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/daemon/screen"
)

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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	d := testDaemon(t)
	_, r := sizedSession(t, d, "rep-plain", 30, 10)
	r.buf.Write([]byte(repairStream()))

	b, cuts, rows, _ := r.buf.ReplayCuts()
	before := string(screen.ReplayCut(b, "screen", cuts, rows))
	plainBefore := repairGet(t, d, "rep-plain", "mode=screen")

	_ = repairGet(t, d, "rep-plain", "repair=report")
	screen.RepairReport(b, cuts, rows)

	if after := string(screen.ReplayCut(b, "screen", cuts, rows)); after != before {
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
