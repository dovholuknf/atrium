package daemon

import "testing"

func TestRevokedLoginReadsTheLineClaudeDraws(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name, text string
		hit        bool
	}{
		{"revoked", "  ⎿  API Error: 401\n  OAuth token revoked · Please run /login\n", true},
		{"expired", "OAuth token has expired · Please run /login\n", true},
		{"expired without the instruction", "Your session has expired\n", false},
		{"idle prompt", "❯ \n─────\n  ⏵⏵ auto mode on\n", false},
		{"the instruction alone", "Please run /login to continue\n", false},
		{"empty", "", false},
	}
	for _, c := range cases {
		if got := revokedLogin(c.text) != ""; got != c.hit {
			t.Errorf("%s: got %v, wanted %v", c.name, got, c.hit)
		}
	}
}
