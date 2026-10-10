//go:build integration

package cli

import "testing"

// `--env KEY=VALUE` splits on the first `=` only, and a pair with no key is
// refused rather than sent as an empty name.
func TestLaunchEnvPairsSplitOnTheFirstEquals(t *testing.T) {
	got, err := parseEnvPairs([]string{"A=1", "B=x=y", "C="})
	if err != nil {
		t.Fatal(err)
	}
	if got["A"] != "1" || got["B"] != "x=y" || got["C"] != "" || len(got) != 3 {
		t.Fatalf("got %v", got)
	}
	for _, bad := range []string{"novalue", "=v"} {
		if _, err := parseEnvPairs([]string{bad}); err == nil {
			t.Fatalf("%q should be refused", bad)
		}
	}
}
