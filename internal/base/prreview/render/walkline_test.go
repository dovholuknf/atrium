package render

import "testing"

func TestWalkLineSplitsNameStateAndRest(t *testing.T) {
	cases := []struct {
		line, name, state, rest string
		ok                      bool
	}{
		{"01-med-a.go-L1.txt  open", "01-med-a.go-L1.txt", "open", "", true},
		{"01-med-a b.go-L1.txt  done  2026-10-01T10:00:00Z  https://x.y/z", "01-med-a b.go-L1.txt", "done",
			"2026-10-01T10:00:00Z  https://x.y/z", true},
		{"01-med-a.txt deferred 2026-10-01T10:00Z\r", "01-med-a.txt", "deferred", "2026-10-01T10:00Z", true},
		{"01-med-a.txt", "", "", "", false},
		{"", "", "", "", false},
	}
	for _, c := range cases {
		name, state, rest, ok := WalkLine(c.line)
		if name != c.name || state != c.state || rest != c.rest || ok != c.ok {
			t.Errorf("%q: got %q %q %q %v", c.line, name, state, rest, ok)
		}
	}
}
