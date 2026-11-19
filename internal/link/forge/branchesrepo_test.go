package forge

import "testing"

func TestBranchesRepoAnswersABareLocalNameRatherThanA400(t *testing.T) {
	for _, c := range []struct {
		in, repo     string
		local, isErr bool
	}{
		{"", "", false, false},
		{"emptyrepo", "", true, false},
		{"github/o/r", "github/o/r", false, false},
		{"o/r", "github/o/r", false, false},
		{"a/b/c/d", "", false, true},
		{"..", "", false, true},
		{"c:/x", "", false, true},
	} {
		repo, local, err := branchesRepo(c.in)
		if repo != c.repo || local != c.local || (err != nil) != c.isErr {
			t.Errorf("%q: got %q %v %v", c.in, repo, local, err)
		}
	}
}
