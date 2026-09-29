package daemon

import (
	"fmt"
	"strings"
)

// Trace fixtures for TestScreenAgainstXterm. Each one is the byte shape a real
// runner or ConPTY produced, cut down to what makes it distinctive. They are
// written as Go strings so the escapes stay readable next to the reason they
// are there.

func repeatLines(n int, f func(i int) string) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		b.WriteString(f(i))
	}
	return b.String()
}

// clearKeepsHistory is the one difference every clear shares. eraseDisplay files
// the rows a clear removes into history on purpose: a transcript that forgot
// everything before the last clear would be worse than one that keeps it, and
// this model exists to produce a transcript. A terminal, and xterm.js, discards
// them (CSI 3 J even drops the scrollback that was already there).
var clearKeepsHistory = map[diffKind]string{
	diffText:     "cleared rows are kept as history here and discarded by xterm.js",
	diffScrolled: "same reason",
}

func traceCases() []traceCase {
	// A Claude Code start: title, hide cursor, kitty keyboard push, modifyOtherKeys,
	// a bordered banner in colour, then the prompt box with the cursor parked
	// inside it.
	banner := "\x1b]0;claude\x07\x1b[?25l\x1b[>1u\x1b[>4;2m\x1b[?2004h" +
		"\x1b[38;2;215;119;87m╭───────────────╮\x1b[m\r\n" +
		"\x1b[38;2;215;119;87m│\x1b[m \x1b[1mWelcome\x1b[m       \x1b[38;2;215;119;87m│\x1b[m\r\n" +
		"\x1b[38;2;215;119;87m╰───────────────╯\x1b[m\r\n\r\n" +
		"\x1b[2m────────────────────────────\x1b[m\r\n" +
		"> \r\n" +
		"\x1b[2m────────────────────────────\x1b[m\x1b[2A\x1b[3G\x1b[?25h"

	// Kitty push and pop around output. Read as plain `u` and `m` these were a
	// cursor restore and dim underline, so the next redraw landed on the banner.
	kitty := "line one\r\nline two\r\n\x1b[s" + "\x1b[>1u" + "typed\x1b[<u" + "\x1b[>4;2m\x1b[?u\x1b[=5;1u" + " more\r\nline three\r\n"

	// Ctrl-delete: the runner turns a key mode on, then redraws the input line in
	// place with erase, carriage return and a fresh prompt.
	ctrlDelete := "> hello world\x1b[>4;2m\x1b[<u\x1b[2K\r> hello \x1b[?25h\x1b[6D\x1b[6C"

	// Full-width coloured diff lines, ConPTY's shape, more of them than rows so
	// some scroll off.
	diff := repeatLines(30, func(i int) string { return conptyLine(i, 60) })

	// Alternate screen: a pager draws, is left, and none of it is history.
	alt := "before\r\n\x1b[?1049h\x1b[H\x1b[2Jpager line 1\r\npager line 2\x1b[10;5Hstatus\x1b[?1049l" + "after\r\n"

	// Bracketed paste: the mode is set, the paste markers arrive with the text,
	// the mode is cleared. None of it draws.
	paste := "\x1b[?2004h> \x1b[200~pasted text\x1b[201~\x1b[?2004l\r\n"

	// A width change mid stream, the shape a restart leaves: 60 wide lines, a
	// mark, then 48 wide ones. Nothing is long enough to wrap on either side, so
	// reflow (accepted below) does not enter into it.
	first := repeatLines(4, func(i int) string { return fmt.Sprintf("wide %d %s\r\n", i, strings.Repeat("=", 40)) })
	second := repeatLines(4, func(i int) string { return fmt.Sprintf("narrow %d %s\r\n", i, strings.Repeat("-", 30)) })

	// Long output, then a bare home and a repaint of the whole screen with no
	// line feed anywhere. ConPTY emits this, and it lands on rows that are still
	// on the live screen. What each model does with that is the point of the
	// fixture. Item 74 owns the behaviour.
	long := repeatLines(60, func(i int) string { return fmt.Sprintf("output line %d\r\n", i) })
	repaint := "\x1b[H" + repeatLines(10, func(i int) string { return fmt.Sprintf("repaint %d\x1b[K\x1b[B\x1b[G", i) })

	// Things a terminal does that screen.go might not. Each is small so a
	// difference names its own cause.
	wrap := strings.Repeat("a", 45) + "\r\n" + strings.Repeat("b", 40) + "\r\nnext\r\n" + strings.Repeat("c", 85) + "x"
	tabs := "a\tb\t\tc\r\n12345678901234567890\x1b[3D\tX\r\n"
	edit := "abcdefghij\x1b[4D\x1b[2P|\x1b[3@--\x1b[2X\r\nline\x1b[1;3H\x1b[1K.\x1b[2;1H\x1b[2K"
	lineOps := "one\r\ntwo\r\nthree\r\nfour\x1b[2;1H\x1b[L>inserted\x1b[3;1H\x1b[M"
	scrollOps := "1\r\n2\r\n3\r\n4\r\n5\x1b[2S\x1b[1T"
	saveRestore := "top\r\n\x1b7\x1b[31mred\x1b[2;10Hmoved\x1b8back\x1b[m\r\n\x1b[s\x1b[5;5Hxx\x1b[uyy"
	reverseIndex := "a\r\nb\r\nc\x1b[1;1H\x1bMtop\r\n"
	eraseDisplay := "aaa\r\nbbb\r\nccc\r\nddd\x1b[2;2H\x1b[J\x1b[H\x1b[1J"
	clear2 := "history1\r\nhistory2\r\nhistory3\x1b[2J\x1b[Hafter clear\r\n"
	clear3 := "history1\r\nhistory2\x1b[3J\x1b[2J\x1b[Hafter\r\n"
	reset := "before\r\n\x1bcafter reset\r\n"
	region := "\x1b[1;4r" + repeatLines(8, func(i int) string { return fmt.Sprintf("region %d\r\n", i) })
	// Scroll regions. Rows are numbered so a wrong one is named by its text.
	fill6 := "r1\r\nr2\r\nr3\r\nr4\r\nr5\r\nr6"
	regionMid := fill6 + "\x1b[2;4r\x1b[4;1H\r\nnew a\r\nnew b\r\n"
	regionFooter := "\x1b[1;7r" + repeatLines(12, func(i int) string { return fmt.Sprintf("row %d\r\n", i) }) +
		"\x1b[8;1Hfooter"
	regionBad := fill6 + "\x1b[3;3r\x1b[4;2r\x1b[3;99r\x1b[9;12r\x1b[2;1Hx"
	regionReset := fill6 + "\x1b[2;3r\x1b[r\x1b[5;1H\r\ntail\r\n"
	regionRI := fill6 + "\x1b[2;4r\x1b[2;1H\x1bMtop\x1b[M\x1b[1;1H\x1bM"
	regionBelow := fill6 + "\x1b[2;4r\x1b[6;1H\r\nlast\r\n\x1b[1;1H\x1bM"
	regionST := fill6 + "\x1b[1;4r\x1b[2S\x1b[1T"
	regionSMid := fill6 + "\x1b[2;5r\x1b[2S\x1b[1T\x1b[S"
	regionIL := "a1\r\na2\r\na3\r\na4\r\na5\r\na6\r\na7\r\na8\x1b[2;5r\x1b[3;1H\x1b[2Lins\x1b[4;1H\x1b[M\x1b[9L"
	regionILOut := "a1\r\na2\r\na3\r\na4\r\na5\r\na6\r\na7\r\na8\x1b[2;5r\x1b[7;1H\x1b[Lz\x1b[1;1H\x1b[Mq"
	regionAlt := fill6 + "\x1b[2;4r\x1b[?1049h\x1b[H\x1b[2Jalt\r\nalt2\x1b[?1049l\x1b[4;1H\r\nafter\r\n"
	regionRIS := fill6 + "\x1b[2;4r\x1bc" + "a\r\nb\r\nc\r\nd\r\ne\r\nf\r\n"
	regionResize1 := fill6 + "\x1b[2;4r"
	regionResize2 := "\x1b[8;1Hlow\r\nx\r\ny\r\n"

	wide := "ab中文cd\r\nあいう\x1b[2D.\r\n"
	sgrKinds := "\x1b[1mbold\x1b[22m \x1b[3mital\x1b[23m \x1b[4munder\x1b[24m \x1b[7minv\x1b[27m \x1b[38;5;12mc256\x1b[39m \x1b[48;2;1;2;3mbg\x1b[49m plain\r\n"
	bgErase := "\x1b[44mblue\x1b[K\x1b[m\r\n\x1b[41m\x1b[2Kred line\x1b[m\r\nend"
	crOverwrite := "hello world\rHELLO\r\nspinner |\b/\b-\b\\\r\n"
	utf8 := "café über ── \U0001F600 x\r\n"

	// The same repaint the way ConPTY really sends it: a bare home and then whole
	// rows written end to end, each exactly the width, with no line feed and no
	// cursor move between them. The wrap does the work of moving down.
	fullRows := "\x1b[H" + repeatLines(11, func(i int) string { return fmt.Sprintf("%-40s", fmt.Sprintf("repaint row %d", i)) })

	// A window narrowed with content on it. xterm.js reflows: a row wider than
	// the new width is re-wrapped onto the next. screen.go cuts it, like a
	// terminal without reflow.
	shrinkFirst := "0123456789012345678901234567890123456789012345\r\nshort\r\n"

	return []traceCase{
		{name: "bare home repaint by autowrap", cols: 40, rows: 12, data: long + fullRows},
		{name: "width shrinks over a long row", cols: 60, rows: 6, data: shrinkFirst + "after\r\n",
			cuts: []sizeCut{{at: len(shrinkFirst), cols: 40}},
			accept: map[diffKind]string{
				diffText:   "xterm.js reflows a row wider than the new width onto a second row, and screen.go cuts it. This model has no reflow, deliberately: the ring's width marks already replay each run at the width it was composed for (applyCuts), which is the case reflow would be for",
				diffCursor: "the reflowed row moves the cursor down a row",
			}},
		{name: "autowrap", cols: 40, rows: 8, data: wrap},
		{name: "tabs", cols: 40, rows: 8, data: tabs},
		{name: "insert delete erase chars", cols: 40, rows: 8, data: edit},
		{name: "insert delete lines", cols: 40, rows: 6, data: lineOps},
		{name: "scroll up and down", cols: 40, rows: 4, data: scrollOps,
			accept: map[diffKind]string{
				diffText:     "CSI S files the rows it scrolls off into history here, as a line feed would. xterm.js discards them. Nothing atrium replays sends CSI S, and keeping text is the safe way to be wrong",
				diffScrolled: "same reason",
			}},
		{name: "save restore cursor", cols: 40, rows: 8, data: saveRestore},
		{name: "reverse index", cols: 40, rows: 4, data: reverseIndex},
		{name: "erase display", cols: 40, rows: 6, data: eraseDisplay},
		{name: "clear screen", cols: 40, rows: 6, data: clear2, accept: clearKeepsHistory},
		{name: "clear scrollback", cols: 40, rows: 3, data: clear3, accept: clearKeepsHistory},
		{name: "full reset", cols: 40, rows: 6, data: reset, accept: clearKeepsHistory},
		{name: "scroll region", cols: 40, rows: 6, data: region},
		{name: "region below the top discards", cols: 40, rows: 6, data: regionMid},
		{name: "region with a footer", cols: 40, rows: 8, data: regionFooter},
		{name: "region ignored when invalid", cols: 40, rows: 6, data: regionBad},
		{name: "region reset by bare r", cols: 40, rows: 5, data: regionReset},
		{name: "reverse index at region top", cols: 40, rows: 6, data: regionRI},
		{name: "index below the region", cols: 40, rows: 6, data: regionBelow},
		{name: "CSI S and T in a region", cols: 40, rows: 6, data: regionST,
			accept: map[diffKind]string{
				diffText:     "CSI S files the rows it scrolls off into history here when the region starts at the top, as a line feed does. xterm.js discards them. The same accepted difference as scroll up and down",
				diffScrolled: "same reason",
			}},
		{name: "CSI S in a region below the top", cols: 40, rows: 6, data: regionSMid},
		{name: "insert delete lines in a region", cols: 40, rows: 8, data: regionIL},
		{name: "insert delete lines outside a region", cols: 40, rows: 8, data: regionILOut},
		{name: "region survives the alt screen", cols: 40, rows: 6, data: regionAlt},
		{name: "region reset by a full reset", cols: 40, rows: 5, data: regionRIS, accept: clearKeepsHistory},
		{name: "region reset by a resize", cols: 40, rows: 6, data: regionResize1 + regionResize2,
			cuts: []sizeCut{{at: len(regionResize1), cols: 40, rows: 8}}},
		{name: "wide characters", cols: 40, rows: 6, data: wide,
			skip: "backlog-2 82: screen.go gives every rune one cell and xterm.js gives CJK two, so a cursor move back over a wide character lands on the wrong column"},
		{name: "attribute kinds", cols: 60, rows: 6, data: sgrKinds},
		{name: "erase with background", cols: 40, rows: 6, data: bgErase},
		{name: "carriage return overwrite", cols: 40, rows: 6, data: crOverwrite},
		{name: "utf8", cols: 40, rows: 6, data: utf8},

		{name: "banner", cols: 60, rows: 24, data: banner},
		{name: "kitty push and pop", cols: 40, rows: 10, data: kitty},
		{name: "ctrl-delete", cols: 40, rows: 10, data: ctrlDelete},
		{name: "full width coloured diff lines", cols: 60, rows: 24, data: diff},
		{name: "alt screen", cols: 40, rows: 12, data: alt},
		{name: "bracketed paste", cols: 40, rows: 10, data: paste},
		{name: "width change mid stream", cols: 60, rows: 12, data: first + second,
			cuts: []sizeCut{{at: len(first), cols: 48}}},
		{name: "bare home repaint over long output", cols: 40, rows: 12, data: long + repaint},
	}
}
