package safepath

import "testing"

func TestNoNULKeepsAPathAndRefusesANUL(t *testing.T) {
	if got := NoNUL("/a/b c"); got != "/a/b c" {
		t.Fatalf("got %q", got)
	}
	if got := NoNUL(""); got != "" {
		t.Fatalf("got %q", got)
	}
	if got := NoNUL("/a\x00b"); got != "\x00" {
		t.Fatalf("got %q", got)
	}
	got := NoNULs([]string{"x", "y\x00"})
	if len(got) != 2 || got[0] != "x" || got[1] != "\x00" {
		t.Fatalf("got %q", got)
	}
}
