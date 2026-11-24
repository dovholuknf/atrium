package screen

import "testing"

func TestAnAiderBannerIsTypedWithoutColourCodes(t *testing.T) {
	if !ReadsSGRAsMouse("aider") || !ReadsSGRAsMouse("t-aider") || ReadsSGRAsMouse("claude") {
		t.Fatal("every row but claude is typed to bare")
	}
	if got := string(StripSGR([]byte("\x1b[38;5;244m[atrium] @a says: \x1b[0m"))); got != "[atrium] @a says: " {
		t.Fatalf("got %q", got)
	}
}
