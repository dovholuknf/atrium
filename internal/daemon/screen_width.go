package daemon

import "sort"

// runeWidth is how many cells xterm.js gives a character: 0, 1 or 2.
//
// IT IS XTERM'S ANSWER, NOT UNICODE'S. The board loads no unicode addon, so the
// terminal a viewer attaches with runs xterm's default UnicodeV6 provider, and a
// replay only lines up if this agrees with it cell for cell. That is why this is
// not golang.org/x/text/width or go-runewidth: both are newer and give an astral
// emoji two cells where the board gives it one, which puts every later column of
// the row off by one per emoji. Nothing here is ambiguous width either. V6 has
// no such class, so those are 1.
//
// The tables are generated from the vendored xterm.js (testdata/gen_widths.js)
// and TestWidthMatchesXterm compares every code point against it, so upgrading
// xterm.js or loading its unicode11 addon fails a test instead of drifting.
func runeWidth(r rune) int {
	if r < 0x7f {
		if r < 0x20 {
			return 0
		}
		return 1
	}
	if inRanges(zeroRanges, r) {
		return 0
	}
	if inRanges(wideRanges, r) {
		return 2
	}
	return 1
}

func inRanges(rs [][2]rune, r rune) bool {
	i := sort.Search(len(rs), func(i int) bool { return rs[i][1] >= r })
	return i < len(rs) && rs[i][0] <= r
}
