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

func TestCardLauncherNamesTheCardItRunsIn(t *testing.T) {
	t.Setenv("ATRIUM_AGENT_NAME", "ogorch")
	t.Setenv("ATRIUM_TASK_ID", "01a1")
	t.Setenv("ATRIUM_ROOM", "sg4")
	if by, id := cardLauncher(); by != "ogorch@sg4" || id != "sg4~01a1" {
		t.Fatalf("inside a card on a room: %q %q", by, id)
	}
	t.Setenv("ATRIUM_ROOM", "")
	if by, id := cardLauncher(); by != "ogorch" || id != "" {
		t.Fatalf("inside a card with no room: %q %q", by, id)
	}
	t.Setenv("ATRIUM_TASK_ID", "")
	if by, id := cardLauncher(); by != "" || id != "" {
		t.Fatalf("outside a card: %q %q", by, id)
	}
}
