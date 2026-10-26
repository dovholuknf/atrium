package screen

import (
	"strings"
	"testing"
)

func TestReplayKeepsInputModes(t *testing.T) {
	for _, rows := range []int{0, 24} {
		s := NewScreenSized(80, rows)
		s.Apply([]byte("\x1b[?1049h\x1b[?1000h\x1b[?1002h\x1b[?1006h\x1b[?2004h\x1b[?1002lframe"))
		got := s.TextWithCursor()
		if !strings.HasSuffix(got, "\x1b[?1000h\x1b[?1006h\x1b[?2004h") {
			t.Fatalf("rows %d: replay %q does not end with the modes still on", rows, got)
		}
		if strings.Contains(got, "\x1b[?1002h") {
			t.Fatalf("rows %d: replay %q turns on a mode the session turned off", rows, got)
		}
	}
}
