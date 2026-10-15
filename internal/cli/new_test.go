package cli

import "testing"

func TestRepoFromRemoteReadsEveryShapeOfOrigin(t *testing.T) {
	for in, want := range map[string]string{
		"https://github.com/openziti/ziti-doc.git": "github.com/openziti/ziti-doc",
		"https://GitHub.com/openziti/ziti/":        "github.com/openziti/ziti",
		"git@github.com:openziti/ziti.git":         "github.com/openziti/ziti",
		"ssh://git@bitbucket.org:7999/o/r.git":     "bitbucket.org/o/r",
	} {
		if got, err := repoFromRemote(in); err != nil || got != want {
			t.Errorf("%s: %q %v, want %s", in, got, err, want)
		}
	}
	for _, bad := range []string{"/srv/git/repo.git", "https://github.com/onlyorg", ""} {
		if got, err := repoFromRemote(bad); err == nil {
			t.Errorf("%q read as %q", bad, got)
		}
	}
}
