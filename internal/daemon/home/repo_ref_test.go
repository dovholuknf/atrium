package home

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestTheHubsOwnSpellingOfARepoIsARef(t *testing.T) {
	t.Parallel()
	for _, repo := range []string{"github/dovholuknf/atrium", "github.com/dovholuknf/atrium", "gitlab.com/o/r"} {
		if !RepoIsRef(repo) {
			t.Errorf("%q was taken for a label", repo)
		}
		req := shared.LaunchRequest{Repo: repo, Branch: "claude/x"}
		if got := HomeRequest(req, "", false); got != store.CardHomeWorktree {
			t.Errorf("a launch of %q asked for home %q, so it fell back to the room's own directory", repo, got)
		}
	}
	for _, repo := range []string{"dovholuknf/atrium", "atrium", "a/b/c"} {
		if RepoIsRef(repo) {
			t.Errorf("%q was taken for a ref", repo)
		}
	}
}
