package store

import "testing"

func TestForgeLabel(t *testing.T) {
	for in, want := range map[string]string{
		"github.com":      "github",
		"GitHub.com":      "github",
		"github":          "github",
		"bitbucket.org":   "bitbucket",
		"gitlab.com":      "gitlab",
		"git.example.com": "git.example.com",
		"":                "",
	} {
		if got := ForgeLabel(in); got != want {
			t.Errorf("ForgeLabel(%q) = %q, want %q", in, got, want)
		}
	}
}
