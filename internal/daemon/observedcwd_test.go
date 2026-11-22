package daemon

import "testing"

func TestObservedCwdRefusesSharesAndClimbs(t *testing.T) {
	cases := map[string]string{
		"":                  "",
		`\\host\share\repo`: "",
		"//host/share/repo": "",
		"C:/work/../other":  "",
		`C:\work\..\other`:  "",
		"/home/me/repo":     "/home/me/repo",
		`C:\dad\work`:       "C:/dad/work",
		"C:/dad/work":       "C:/dad/work",
	}
	for in, want := range cases {
		if got := observedCwd(in); got != want {
			t.Errorf("observedCwd(%q) = %q, want %q", in, got, want)
		}
	}
}
