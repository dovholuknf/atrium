package daemon

import (
	"strings"
	"testing"
)

// A clear is a mark, then a repaint in place that has not yet erased the rows below the new text, then the erase-display a
// restart sends. The page the mark kept is the old one. The 2J that follows lands on a grid that is half new text and half
// the old page's rows, and it kept that too, so old diff rows were filed in history after the start of the reply.
// A separator rule at the same row in the old and the new frame is part of the new page when text follows it, so it stays.
func TestASeparatorAboveNewTextIsStillFiledAfterAMark(t *testing.T) {
	t.Parallel()
	s := newScreenSized(40, 6)
	rule := strings.Repeat("─", 10)
	s.apply([]byte("old 1\r\n" + rule + "\r\nold 3"))
	s.apply([]byte(clearMarkOSC))
	s.apply([]byte("\x1b[Hnew 1\x1b[K\r\n" + rule + "\r\nnew 3\x1b[K\r\n"))
	s.apply([]byte("\x1b[2J\x1b[Hlater\r\n"))
	if n := strings.Count(plain(s.text()), rule); n != 2 {
		t.Fatalf("the rule is in history %d times, want once per page:\n%q", n, plain(s.text()))
	}
}

func TestAnEraseDisplayMidRepaintDoesNotSpliceTheOldPageAfterTheNewText(t *testing.T) {
	t.Parallel()
	s := newScreenSized(40, 6)
	s.apply([]byte("diff 1\r\ndiff 2\r\ndiff 3\r\ndiff 4\r\ndiff 5"))
	s.apply([]byte(clearMarkOSC))
	s.apply([]byte("\x1b[Hreply 1\x1b[K\r\nreply 2\x1b[K\r\n"))
	s.apply([]byte("\x1b[2J\x1b[Hreply 1\r\nreply 2\r\nreply 3\r\n"))
	got := plain(s.text())
	if n := strings.Count(got, "diff 4"); n != 1 {
		t.Fatalf("diff 4 is in history %d times, not once:\n%q", n, got)
	}
	if strings.LastIndex(got, "diff 4") > strings.Index(got, "reply 1") {
		t.Fatalf("an old diff row was filed after the reply began:\n%q", got)
	}
}
