package daemon

import (
	"strings"
	"testing"
)

func TestRingKeepsInputModesAfterTheEnableIsOverwritten(t *testing.T) {
	r := newRing(64, 80)
	_, _ = r.Write([]byte("\x1b[?1003h\x1b[?1006h"))
	_, _ = r.Write([]byte(strings.Repeat("x", 500)))
	if got := r.InputModes(); got != "\x1b[?1003h\x1b[?1006h" {
		t.Fatalf("InputModes() = %q", got)
	}
}
