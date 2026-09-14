package daemon

import (
	"strconv"
	"strings"
	"testing"
)

// Position-based output must become separate lines. This fixture comes from
// a captured file listing that previously flattened into one long line.

// block draws rows the way claude-code does: position, write, position, write,
// and not one newline anywhere in it.
func block(rows ...string) string {
	var b strings.Builder
	for i, r := range rows {
		b.WriteString("\x1b[" + strconv.Itoa(20+i) + ";3H")
		b.WriteString(r)
	}
	return b.String()
}

func TestARowJumpBecomesALineEnding(t *testing.T) {
	in := block(
		`1 #!/usr/bin/env bash`,
		`2 out="D:/tmp/b2-01"`,
		`3 browser="C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe"`,
		`4 rm -rf "$out/prof2"`,
	)

	got := string(flatten([]byte(in)))
	lines := strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n")

	var seen []string
	for _, l := range lines {
		if s := strings.TrimSpace(l); s != "" {
			seen = append(seen, s)
		}
	}
	if len(seen) != 4 {
		t.Fatalf("a four row block came back as %d lines:\n%q", len(seen), got)
	}
	// The exact failure from the capture: everything on one line.
	if strings.Contains(got, `bash  2 out=`) || strings.Contains(got, "bash2 out=") {
		t.Fatalf("the rows ran together: %q", got)
	}
	for i, want := range []string{"1 #!/usr/bin/env bash", "2 out=", "3 browser=", "4 rm -rf"} {
		if !strings.HasPrefix(seen[i], want) {
			t.Fatalf("line %d is %q, wanted it to start %q", i+1, seen[i], want)
		}
	}
}

// The column becomes leading spaces, so an indented row comes back indented.
func TestAColumnBecomesIndent(t *testing.T) {
	got := string(flatten([]byte("top\x1b[2;5Hindented")))
	if !strings.Contains(got, "\n    indented") {
		t.Fatalf("the indent was lost: %q", got)
	}
}

// Backward and same-row moves must not overwrite previously emitted output.
func TestARowJumpBackwardsDrawsNothing(t *testing.T) {
	in := "\x1b[5;1Hfifth row" + "\x1b[2;1Hsecond row, drawn later"

	got := string(flatten([]byte(in)))

	if !strings.Contains(got, "fifth row") {
		t.Fatalf("the earlier row was overwritten: %q", got)
	}
	if strings.Count(got, "\n") > 5 {
		t.Fatalf("going backwards added line endings: %q", got)
	}
}

// A forward jump contributes one line ending regardless of its distance.
func TestARowJumpIsAlwaysOneBreak(t *testing.T) {
	for _, in := range []string{
		"top\x1b[2;1Hnext",   // the very next row
		"top\x1b[8;1Hnext",   // a few rows down
		"top\x1b[400;1Hnext", // a full screen repaint
	} {
		got := string(flatten([]byte(in)))
		if n := strings.Count(got, "\n"); n != 1 {
			t.Fatalf("%q produced %d line endings: %q", in, n, got)
		}
		if !strings.Contains(got, "top") || !strings.Contains(got, "next") {
			t.Fatalf("text either side was lost: %q", got)
		}
	}
}

// Cursor moves must not add blank lines. Preserve a real newline between
// blocks; repeated blank lines are tested separately.
func TestARowMoveAddsNoBlankLine(t *testing.T) {
	got := string(flatten([]byte("one\r\n\r\ntwo")))
	if n := strings.Count(got, "\n"); n != 2 {
		t.Fatalf("the separating blank line was changed: %q", got)
	}
	if !strings.Contains(got, "one") || !strings.Contains(got, "two") {
		t.Fatalf("text was lost: %q", got)
	}
}

// A real newline advances the row too, or the next positioning computes its
// gap from a row the text has already passed.
func TestARealNewlineAdvancesTheRow(t *testing.T) {
	got := string(flatten([]byte("one\r\ntwo\x1b[2;1Hthree")))
	// Row 2 has already been reached by the newline, so this positioning is
	// backwards and draws nothing.
	if strings.Count(got, "\n") != 1 {
		t.Fatalf("the row counter did not follow the newline: %q", got)
	}
}

// A private mode is not a position, whatever its final byte looks like.
func TestAPrivateModeIsNotAPosition(t *testing.T) {
	got := string(flatten([]byte("kept\x1b[?1049hstill kept")))
	if strings.Contains(got, "\n") {
		t.Fatalf("a private mode was read as a cursor move: %q", got)
	}
	if got != "keptstill kept" {
		t.Fatalf("got %q", got)
	}
}

// Collapse repeated blank rows left by screen redraws while keeping a
// single blank line between blocks.
func TestARunOfBlankLinesBecomesOne(t *testing.T) {
	got := string(flatten([]byte("above\r\n\r\n\r\n\r\n\r\n\r\n\r\nbelow")))
	if n := strings.Count(got, "\n"); n != 2 {
		t.Fatalf("a run of seven blank lines came out as %d line endings: %q", n, got)
	}
	if !strings.Contains(got, "above") || !strings.Contains(got, "below") {
		t.Fatalf("text either side was lost: %q", got)
	}
}

// ONE IS KEPT. A blank line between blocks is how the runner separates them,
// and removing it runs them together.
func TestOneBlankLineSurvives(t *testing.T) {
	got := string(flatten([]byte("above\r\n\r\nbelow")))
	if !strings.Contains(got, "above\r\n\r\nbelow") {
		t.Fatalf("the separating blank line was removed: %q", got)
	}
}

// Treat lines containing only colour changes and spacing as blank.
func TestALineOfOnlyColourCountsAsBlank(t *testing.T) {
	in := "above\r\n" + "\x1b[m   \r\n" + "\x1b[38;2;1;2;3m \r\n" + "   \r\n" + "below"
	got := string(flatten([]byte(in)))

	blank := 0
	for _, l := range strings.Split(strings.ReplaceAll(got, "\r\n", "\n"), "\n") {
		if visuallyEmpty([]byte(l)) {
			blank++
		}
	}
	if blank > 1 {
		t.Fatalf("%d blank-looking lines survived: %q", blank, got)
	}
}
