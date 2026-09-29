package daemon

import "testing"

// A spinner line redrawn with a VS16 must spend one table entry, not one per
// redraw, so a long session does not fill the cap before real text needs it.
func TestCombiningMarksAreInterned(t *testing.T) {
	s := newScreenSized(40, 4)
	for i := 0; i < 100000; i++ {
		s.apply([]byte("\r⠋ working️"))
	}
	s.apply([]byte("\r\nकु"))
	if len(s.combs) != 2 {
		t.Fatalf("%d table entries, want 2: %q", len(s.combs), s.combs)
	}
	if got := texts(s)[1]; got != "कु" {
		t.Fatalf("devanagari line %q lost its marks", got)
	}
}

// Past the cap a sequence already held still attaches and a new one is dropped.
func TestCombiningMarksPastTheCap(t *testing.T) {
	s := newScreenSized(40, 4)
	first := string([]rune{0x300, 0x300, 0x300})
	// Every sequence of three marks from U+0300..U+036F, until the table is full.
fill:
	for a := rune(0x300); a < 0x370; a++ {
		for b := rune(0x300); b < 0x370; b++ {
			for c := rune(0x300); c < 0x370; c++ {
				if len(s.combs) >= combsMaxKept {
					break fill
				}
				s.apply([]byte("\rx" + string([]rune{a, b, c})))
			}
		}
	}
	if len(s.combs) != combsMaxKept {
		t.Fatalf("%d entries, want %d", len(s.combs), combsMaxKept)
	}
	s.apply([]byte("\r\nb" + first))
	if got := texts(s)[1]; got != "b"+first {
		t.Fatalf("interned sequence lost past the cap: %q", got)
	}
	s.apply([]byte("\r\nc⃐⃑"))
	if got := texts(s)[2]; got != "c" {
		t.Fatalf("new sequence kept past the cap: %q", got)
	}
	if len(s.combs) != combsMaxKept {
		t.Fatalf("table grew past the cap: %d", len(s.combs))
	}
}
