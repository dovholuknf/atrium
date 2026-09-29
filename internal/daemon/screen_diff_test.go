package daemon

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"testing"
)

// screen.go is atrium's own terminal model and the board draws the same bytes
// with the vendored xterm.js. This file feeds one trace to both and compares
// what they end up holding: every row (scrollback then viewport), which cells
// carry any attribute, where the cursor is, and how many rows scrolled off.
//
// It shells out to node with testdata/xterm_dump.js, which loads
// internal/api/web/vendor/xterm.js and never opens a DOM. No node, no test: the
// skip says so. Nothing here reaches for npm or a CDN copy of xterm.
//
// HOW THE TWO ARE MADE COMPARABLE
//
//   - Trailing blanks are trimmed on both. xterm pads a row to the width and
//     screen.go does not, and neither is content.
//   - Trailing blank ROWS are trimmed on both, for the same reason.
//   - Attributes are compared as a mask, styled or default per character, not
//     as colours. screen.go keeps its SGR as a string it replays verbatim and
//     has no colour model, so "which cells are coloured" is the question it can
//     answer. A cell that is styled in one and default in the other is a
//     difference; two different colours are not one.
//   - The cursor is compared inside the viewport (row and column), and the
//     number of rows that scrolled off is compared separately, because the
//     replay's absolute moves depend on both.
//
// ACCEPTED DIFFERENCES, each written where it is claimed (see `accept` below):
// they are places where screen.go deliberately does less than a terminal. Any
// difference that is not claimed fails the test.

// traceCase is one trace and the size it was drawn at.
type traceCase struct {
	name       string
	cols, rows int
	cuts       []sizeCut
	data       string
	// accept names the differences this trace is allowed to have, each with
	// the reason. A name that is claimed and does not occur fails the test, so
	// an accepted difference that gets fixed has to be un-claimed.
	accept map[diffKind]string
	// skip marks a difference that is a real bug in screen.go and not a small
	// fix. The case still runs: it skips with the diff while the two disagree,
	// and fails once they agree so the marker gets removed.
	skip string
}

type diffKind string

const (
	diffText     diffKind = "text"
	diffMask     diffKind = "mask"
	diffCursor   diffKind = "cursor"
	diffScrolled diffKind = "scrolled"
)

type xtermLine struct {
	T string `json:"t"`
	S string `json:"s"`
}

type xtermDump struct {
	Lines []xtermLine `json:"lines"`
	BaseY int         `json:"baseY"`
	CurY  int         `json:"curY"`
	CurX  int         `json:"curX"`
}

// runXterm renders a trace through the vendored xterm.js, or skips the test
// when node is not installed.
func runXterm(t *testing.T, c traceCase) xtermDump {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH: the screen.go against xterm.js differential needs it (testdata/xterm_dump.js)")
	}
	job, _ := json.Marshal(map[string]any{
		"cols": c.cols, "rows": c.rows,
		"cuts": cutsJSON(c.cuts),
		"data": base64.StdEncoding.EncodeToString([]byte(c.data)),
	})
	cmd := exec.Command(node, "testdata/xterm_dump.js")
	cmd.Stdin = bytes.NewReader(job)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		t.Fatalf("node xterm_dump.js: %v\n%s", err, errb.String())
	}
	var d xtermDump
	if err := json.Unmarshal(out.Bytes(), &d); err != nil {
		t.Fatalf("xterm_dump.js output: %v\n%s", err, out.String())
	}
	return d
}

func cutsJSON(cuts []sizeCut) []map[string]int {
	out := []map[string]int{}
	for _, c := range cuts {
		out = append(out, map[string]int{"at": c.at, "cols": c.cols, "rows": c.rows})
	}
	return out
}

// dumpScreen is what screen.go holds, in the shape xterm_dump.js prints.
func dumpScreen(s *screen) xtermDump {
	var d xtermDump
	row := func(r []cell) xtermLine {
		end := len(r)
		for end > 0 && (r[end-1].ch == ' ' || r[end-1].ch == 0) {
			end--
		}
		var tb, mb strings.Builder
		for _, c := range r[:end] {
			ch := c.ch
			if ch == 0 {
				ch = ' '
			}
			tb.WriteRune(ch)
			if c.sgr != "" {
				mb.WriteByte('1')
			} else {
				mb.WriteByte('0')
			}
		}
		return xtermLine{tb.String(), mb.String()}
	}
	for _, r := range s.history {
		d.Lines = append(d.Lines, row(r))
	}
	for _, r := range s.cells {
		d.Lines = append(d.Lines, row(r))
	}
	d.BaseY = len(s.history)
	d.CurY, d.CurX = s.row, s.col
	// A cursor waiting to wrap sits past the last column in xterm.js (x == cols)
	// and on it here, with wrapNext set. The same state, spelled two ways.
	if s.wrapNext {
		d.CurX = s.cols
	}
	return d
}

func trimBlankRows(l []xtermLine) []xtermLine {
	for len(l) > 0 && l[len(l)-1].T == "" {
		l = l[:len(l)-1]
	}
	return l
}

// diffDumps lists every way the two disagree, keyed by kind.
func diffDumps(got, want xtermDump) map[diffKind][]string {
	out := map[diffKind][]string{}
	g, w := trimBlankRows(got.Lines), trimBlankRows(want.Lines)
	for i := 0; i < len(g) || i < len(w); i++ {
		var gl, wl xtermLine
		if i < len(g) {
			gl = g[i]
		}
		if i < len(w) {
			wl = w[i]
		}
		if gl.T != wl.T {
			out[diffText] = append(out[diffText], fmt.Sprintf("row %d\n\t\tscreen.go: %q\n\t\txterm.js:  %q", i, gl.T, wl.T))
		} else if gl.S != wl.S {
			out[diffMask] = append(out[diffMask], fmt.Sprintf("row %d %q\n\t\tscreen.go: %s\n\t\txterm.js:  %s", i, gl.T, gl.S, wl.S))
		}
	}
	if got.CurY != want.CurY || got.CurX != want.CurX {
		out[diffCursor] = append(out[diffCursor], fmt.Sprintf("screen.go (row %d, col %d), xterm.js (row %d, col %d)",
			got.CurY, got.CurX, want.CurY, want.CurX))
	}
	if got.BaseY != want.BaseY {
		out[diffScrolled] = append(out[diffScrolled], fmt.Sprintf("screen.go scrolled %d rows off, xterm.js %d", got.BaseY, want.BaseY))
	}
	return out
}

func TestScreenAgainstXterm(t *testing.T) {
	for _, c := range traceCases() {
		t.Run(c.name, func(t *testing.T) {
			s := newScreenSized(c.cols, c.rows)
			s.applyCuts([]byte(c.data), c.cuts)
			diffs := diffDumps(dumpScreen(s), runXterm(t, c))

			if c.skip != "" {
				if len(diffs) == 0 {
					t.Fatalf("%s: screen.go and xterm.js now agree, so drop the skip", c.skip)
				}
				var all []string
				for kind, list := range diffs {
					all = append(all, fmt.Sprintf("%s: %s", kind, strings.Join(list, "; ")))
				}
				t.Skipf("%s\n%s", c.skip, strings.Join(all, "\n"))
			}

			for kind, list := range diffs {
				if _, ok := c.accept[kind]; ok {
					continue
				}
				t.Errorf("%s differs (%d):\n\t%s", kind, len(list), strings.Join(list, "\n\t"))
			}
			for kind, why := range c.accept {
				if len(diffs[kind]) == 0 {
					t.Errorf("%s is listed as an accepted difference (%s) but the two now agree: drop it", kind, why)
				}
			}
		})
	}
}
