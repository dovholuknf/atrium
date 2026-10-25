package safepath

import (
	"strings"
	"testing"
)

// Containment. Every test here is one of the ways the three rules get broken,
// because a containment check that is right most of the time is a containment
// check that is wrong.

// SafeName. Belt and braces, because the upload path computes its own
// destination, but a name that can mean something is a name worth neutering.
func TestSafeNameStripsEverythingStructural(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"screenshot.png", "screenshot.png"},
		{"../../etc/passwd", "passwd"},
		{`..\..\windows\system32\a.dll`, "a.dll"},
		{"C:evil.txt", "evil.txt"},
		{"/absolute/thing.log", "thing.log"},
		{"..", "file"},
		{"...", "file"},
		{"", "file"},
		{"   ", "file"},
		{".hidden", "hidden"},
		{"a<b>c.txt", "a-b-c.txt"},
		{"tab\there.txt", "tabhere.txt"},
	} {
		if got := SafeName(tc.in); got != tc.want {
			t.Fatalf("SafeName(%q) = %q, wanted %q", tc.in, got, tc.want)
		}
	}
}

// The right-to-left override renders `gpj.exe` as `exe.jpg`, which is the
// oldest trick there is for making an executable look like an image.
func TestSafeNameStripsDirectionOverrides(t *testing.T) {
	got := SafeName("photo‮gpj.exe")
	if strings.ContainsRune(got, '‮') {
		t.Fatalf("a direction override survived: %q", got)
	}
}

// A very long name is cut and keeps its extension, which is the part that
// tells a model what it is looking at.
func TestSafeNameKeepsTheExtensionWhenItTruncates(t *testing.T) {
	got := SafeName(strings.Repeat("a", 400) + ".png")
	if len(got) > 120 {
		t.Fatalf("a long name came back %d characters", len(got))
	}
	if !strings.HasSuffix(got, ".png") {
		t.Fatalf("truncation lost the extension: %q", got)
	}
}
