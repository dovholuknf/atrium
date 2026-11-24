package link

import "testing"

func TestTerminalURL(t *testing.T) {
	for _, c := range []struct{ room, wire, want string }{
		{"sg3", "release-tag-main", "http://hub/sg3/terminal/release-tag-main"},
		{"sg3", "sparta/rnd", "http://hub/sg3/terminal/rnd"},
		{"sg3", "", "http://hub/#term=K1"},
		{"", "rnd", "http://hub/#term=K1"},
	} {
		if got := TerminalURL("http://hub", c.room, c.wire, "K1"); got != c.want {
			t.Errorf("%q %q: %s, want %s", c.room, c.wire, got, c.want)
		}
	}
}
