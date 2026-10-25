package screen

import (
	"strings"
	"testing"
)

func TestStripPasteMarkersSplicedMarker(t *testing.T) {
	t.Parallel()
	in := "\x1b[20" + PasteClose + "1~x"
	if got := StripPasteMarkers(in); strings.Contains(got, "\x1b[20") {
		t.Fatalf("a marker re-formed after stripping: %q", got)
	}
}
