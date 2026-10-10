package daemon

import (
	"strings"
	"testing"
)

func TestStripPasteMarkersSplicedMarker(t *testing.T) {
	in := "\x1b[20" + pasteClose + "1~x"
	if got := stripPasteMarkers(in); strings.Contains(got, "\x1b[20") {
		t.Fatalf("a marker re-formed after stripping: %q", got)
	}
}
