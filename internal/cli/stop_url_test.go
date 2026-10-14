package cli

import "testing"

// `--url 127.0.0.1:7781` is what the rollout steps type, and Go's URL parser
// reads a bare host as a scheme and refuses it.
func TestBoardAddressTakesABareHostPort(t *testing.T) {
	for in, want := range map[string]string{
		"127.0.0.1:7781":         "http://127.0.0.1:7781",
		"http://127.0.0.1:7781/": "http://127.0.0.1:7781",
		"https://h:1":            "https://h:1",
	} {
		if got := boardAddress(in); got != want {
			t.Fatalf("boardAddress(%q) = %q, want %q", in, got, want)
		}
	}
}
