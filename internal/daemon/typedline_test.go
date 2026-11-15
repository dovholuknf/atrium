package daemon

import "testing"

// Whitespace before anything was written holds nothing. Text after it counts from its first non-blank, and text cut
// off the front counts in full because its front is unknown.
func TestLeadingWhitespaceIsNotADraft(t *testing.T) {
	cases := []struct {
		name    string
		text    string
		dropped int
		count   int
	}{
		{"nothing", "", 0, 0},
		{"a space", " ", 0, 0},
		{"tabs and spaces", " \t  ", 0, 0},
		{"non-breaking space", " ", 0, 0},
		{"blanks then a word", "  hi", 0, 2},
		{"a word then blanks", "hi  ", 0, 4},
		{"blanks after a cut front", "  ", 3, 5},
	}
	for _, c := range cases {
		l := &typedLine{text: []rune(c.text), dropped: c.dropped}
		if got := l.count(); got != c.count {
			t.Errorf("%s: count = %d, want %d", c.name, got, c.count)
		}
		if got := l.empty(); got != (c.count == 0) {
			t.Errorf("%s: empty = %v", c.name, got)
		}
	}
	unsure := &typedLine{text: []rune(" "), unsure: "up"}
	if unsure.empty() {
		t.Error("a blank line atrium lost track of reads as empty")
	}
	blanks := &typedLine{text: []rune("  ")}
	if blanks.moved("left"); blanks.unsure == "" {
		t.Error("a cursor key on a line of blanks left it exact")
	}
}
