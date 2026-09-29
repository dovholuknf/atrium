package daemon

import (
	"encoding/json"
	"os/exec"
	"strings"
	"testing"
)

// assertWideInvariant checks the one rule the wide cell ops exist to keep: every
// wide character is followed by exactly one continuation cell, and every
// continuation follows one. Applied to history and to the alternate screen too.
func assertWideInvariant(t *testing.T, s *screen) {
	t.Helper()
	rows := append(append([][]cell{}, s.history...), s.cells...)
	rows = append(rows, s.altCells...)
	for n, r := range rows {
		for i, c := range r {
			switch {
			case c.ch == contCh:
				if i == 0 || r[i-1].ch < 0 || runeWidth(r[i-1].ch) != 2 {
					t.Fatalf("row %d col %d: a continuation with no wide character before it", n, i)
				}
			case c.ch > 0 && runeWidth(c.ch) == 2:
				if i+1 >= len(r) || r[i+1].ch != contCh {
					t.Fatalf("row %d col %d: %q is wide and has no continuation", n, i, c.ch)
				}
			}
		}
	}
}

func TestWidthTable(t *testing.T) {
	for _, c := range []struct {
		r rune
		w int
	}{
		{'a', 1}, {' ', 1}, {'é', 1}, {'─', 1}, {'α', 1},
		{'あ', 2}, {'中', 2}, {'한', 2}, {'Ａ', 2}, {0x20000, 2},
		{0x1F600, 1}, // xterm's V6 table gives astral emoji one cell
		{0x301, 0}, {0x200d, 0}, {0xfe0f, 0},
	} {
		if got := runeWidth(c.r); got != c.w {
			t.Errorf("runeWidth(%U) = %d, want %d", c.r, got, c.w)
		}
	}
}

// TestWidthMatchesXterm compares runeWidth with the vendored xterm.js for every
// code point. It is what stops the table drifting when xterm.js is upgraded, or
// when the board starts loading its unicode11 addon.
func TestWidthMatchesXterm(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH: the width parity check needs it (testdata/gen_widths.js)")
	}
	out, err := exec.Command(node, "testdata/gen_widths.js", "json").Output()
	if err != nil {
		t.Fatalf("gen_widths.js: %v", err)
	}
	var want struct{ Wide, Zero [][2]rune }
	if err := json.Unmarshal(out, &want); err != nil {
		t.Fatal(err)
	}
	w := make([]int8, 0x110000)
	for i := range w {
		w[i] = 1
	}
	for i := 0; i < 0x20; i++ {
		w[i] = 0
	}
	for _, r := range want.Wide {
		for c := r[0]; c <= r[1]; c++ {
			w[c] = 2
		}
	}
	for _, r := range want.Zero {
		for c := r[0]; c <= r[1]; c++ {
			w[c] = 0
		}
	}
	bad := 0
	for c := rune(0); c < 0x110000; c++ {
		if c >= 0xd800 && c <= 0xdfff {
			continue
		}
		if int(w[c]) != runeWidth(c) {
			if bad++; bad <= 10 {
				t.Errorf("runeWidth(%U) = %d, xterm.js says %d", c, runeWidth(c), w[c])
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d code points differ: regenerate with `node testdata/gen_widths.js go screen_width_tables.go`", bad)
	}
}

// texts is the grid as rows of text with the continuation cells skipped.
func texts(s *screen) []string {
	var out []string
	for _, r := range s.cells {
		var b strings.Builder
		for _, c := range r {
			if c.ch != contCh {
				s.emit(&b, c)
			}
		}
		out = append(out, strings.TrimRight(b.String(), " "))
	}
	return out
}

func wideScreen(cols, rows int, data string) *screen {
	s := newScreenSized(cols, rows)
	s.apply([]byte(data))
	return s
}

func TestWideCellsTakeTwoColumns(t *testing.T) {
	s := wideScreen(10, 3, "あいう")
	if s.col != 6 {
		t.Fatalf("cursor at %d after three wide characters, want 6", s.col)
	}
	if s.cells[0][1].ch != contCh || s.cells[0][3].ch != contCh {
		t.Fatalf("no continuation cells: %+v", s.cells[0][:6])
	}
	assertWideInvariant(t, s)
}

func TestWideOverwriteBlanksTheOtherHalf(t *testing.T) {
	for _, c := range []struct{ name, data, want string }{
		{"first half", "あい\rx", "x い"},
		{"second half", "あい\x1b[3Dx", " xい"},
		{"the head of the next", "aあいb\x1b[1;1H\x1b[2Cう", "a う b"},
	} {
		s := wideScreen(10, 2, c.data)
		assertWideInvariant(t, s)
		if got := texts(s)[0]; got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestWideMarksAttachToTheCellBefore(t *testing.T) {
	s := wideScreen(10, 2, "éあ́x́")
	if got, want := texts(s)[0], "éあ́x́"; got != want {
		t.Fatalf("%q, want %q", got, want)
	}
	if s.col != 4 {
		t.Fatalf("marks moved the cursor: col %d, want 4", s.col)
	}
	// A mark with nothing before it stands in a cell of its own, as in xterm.js.
	if got := texts(wideScreen(10, 2, "́y"))[0]; got != "́y" {
		t.Fatalf("%q", got)
	}
}

func TestWideInvariantSurvivesEditing(t *testing.T) {
	for _, data := range []string{
		"あいう\x1b[2G\x1b[P", "あいう\x1b[3G\x1b[P", "あいう\x1b[2G\x1b[@", "あいうえお\x1b[3G\x1b[@",
		"あいう\x1b[2G\x1b[X", "あいう\x1b[3G\x1b[K", "あいう\x1b[3G\x1b[1K", "あいうえお\x1b[9G\x1b[@",
		"abcdeあ", "あいうえおか", "\x1b[?1049hあいう\x1b[2G\x1b[P\x1b[?1049l",
	} {
		assertWideInvariant(t, wideScreen(10, 3, data))
	}
}

func TestWideResizeNeverLeavesAHalf(t *testing.T) {
	for cols := 1; cols <= 8; cols++ {
		s := wideScreen(8, 3, "あいうえ\r\nabあ")
		s.resize(cols)
		assertWideInvariant(t, s)
		if len(s.cells[0]) != cols {
			t.Fatalf("width %d, want %d", len(s.cells[0]), cols)
		}
	}
}

func TestWideTextEmitsTheRuneOnce(t *testing.T) {
	s := wideScreen(10, 0, "あいう\r\nx\x1b[1;3H")
	if got := s.text(); got != "あいう\r\nx\r\n" {
		t.Fatalf("text %q", got)
	}
	// The cursor is a cell column, which is where the terminal puts it once it
	// has drawn each of those characters across two.
	if got := s.textWithCursor(); !strings.HasSuffix(got, "\x1b[2A\r\x1b[2C") {
		t.Fatalf("cursor move in %q", got)
	}
	f := newScreenSized(10, 3)
	f.apply([]byte("あいう\r\nx\x1b[1;3H"))
	if got := f.textAtRows(); !strings.Contains(got, "あいう\r\nx") || !strings.HasSuffix(got, "\x1b[1;3H") {
		t.Fatalf("rows %q", got)
	}
}

func TestReplayOfWideTextLandsOnTheSameColumns(t *testing.T) {
	session := "あいう\x1b[2D.\r\nabあ\x1b[1D\x1b[K"
	want := newScreenSized(10, 4)
	want.apply([]byte(session))
	got := newScreenSized(10, 4)
	got.apply(Replay([]byte(session), "screen", 10, 4))
	if w, g := strings.Join(texts(want), "|"), strings.Join(texts(got), "|"); w != g {
		t.Fatalf("replay differs: %q, want %q", g, w)
	}
	if got.row != want.row || got.col != want.col {
		t.Fatalf("cursor %d,%d, want %d,%d", got.row, got.col, want.row, want.col)
	}
}

func TestDecodeRuneReadsOneBadByteAsOne(t *testing.T) {
	if r, n := decodeRune([]byte{0xe3, 'a', 'b'}); r != 0xfffd || n != 1 {
		t.Fatalf("got %U, %d", r, n)
	}
	if r, n := decodeRune([]byte("あa")); r != 'あ' || n != 3 {
		t.Fatalf("got %U, %d", r, n)
	}
}

func BenchmarkScreenApplyCJK(b *testing.B) {
	data := []byte(strings.Repeat("日本語のテキストです。abc def 中文字符\r\n", 200))
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		s := newScreenSized(80, 24)
		s.apply(data)
	}
}

func BenchmarkScreenApplyASCII(b *testing.B) {
	data := []byte(strings.Repeat("the quick brown fox jumps over the lazy dog 0123456789\r\n", 200))
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		s := newScreenSized(80, 24)
		s.apply(data)
	}
}
