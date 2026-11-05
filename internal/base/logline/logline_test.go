package logline

import "testing"

func TestSDropsLineBreaks(t *testing.T) {
	if got := S("a\r\nb\nc"); got != "abc" {
		t.Fatalf("got %q", got)
	}
	if got := S(42); got != "42" {
		t.Fatalf("got %q", got)
	}
}
