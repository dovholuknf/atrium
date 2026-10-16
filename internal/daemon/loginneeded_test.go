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

func TestRevokedLoginIgnoresQuotesOfTheLine(t *testing.T) {
	t.Parallel()
	for _, text := range []string{
		"+\tOAuth token revoked · Please run /login\n",
		"the card said: OAuth token revoked · Please run /login\n",
		"> OAuth token revoked · Please run /login\n",
		"OAuth token revoked\n",
	} {
		if got := revokedLogin(text); got != "" {
			t.Errorf("%q matched as %q", text, got)
		}
	}
	if revokedLogin("  ⎿  OAuth token revoked · Please run /login") == "" {
		t.Error("the glyph-prefixed line did not match")
	}
}

func TestLiftingTheFlagAcknowledgesTheCachedVerdicts(t *testing.T) {
	t.Parallel()
	var st loginState
	st.on = true
	st.verdicts = map[string]loginVerdict{
		"a": {out: 1, line: "OAuth token revoked · Please run /login"},
		"b": {out: 2},
	}
	st.ackVerdicts()
	if !st.verdicts["a"].acked || st.verdicts["b"].acked {
		t.Errorf("acked wrong: %+v", st.verdicts)
	}
}
