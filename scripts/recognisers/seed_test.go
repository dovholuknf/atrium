package recognisers

import (
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// THE BUILT-IN ROWS READ AND RECOGNISE A PULL REQUEST however it was copied: the PR page, its changes tab, its files
// tab, a comment anchor.
func TestTheSeedRecognisesAGitHubPullRequest(t *testing.T) {
	seed, err := Seed()
	if err != nil {
		t.Fatal(err)
	}
	if len(seed) == 0 {
		t.Fatal("the seed is empty")
	}
	rows := make([]*store.Recogniser, 0, len(seed))
	for i := range seed {
		rows = append(rows, &seed[i])
	}
	for _, url := range []string{
		"https://github.com/openziti/ziti-console/pull/967",
		"https://github.com/openziti/ziti-console/pull/967/changes",
		"https://github.com/openziti/ziti-console/pull/967/files",
		"https://github.com/openziti/ziti-console/pull/967#issuecomment-1",
	} {
		r, vars, err := store.MatchRecogniserIn(rows, url)
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		if r.ID != "github-pull-request" || vars["host"] != "github.com" || vars["org"] != "openziti" ||
			vars["repo"] != "ziti-console" || vars["num"] != "967" {
			t.Fatalf("%s: matched %s with %v", url, r.ID, vars)
		}
	}
}

// Every kind clint pastes has a seed row: a Bitbucket PR, a Zendesk ticket, a Discourse topic.
func TestTheSeedCoversEveryKindPasted(t *testing.T) {
	seed, err := Seed()
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]*store.Recogniser, 0, len(seed))
	for i := range seed {
		rows = append(rows, &seed[i])
	}
	for _, url := range []string{
		"https://bitbucket.org/acme/widget/pull-requests/12",
		"https://github.com/openziti/ziti/issues/4211",
		"https://netfoundry.zendesk.com/agent/tickets/12345",
		"https://openziti.discourse.group/t/some-topic/4567",
	} {
		if _, _, err := store.MatchRecogniserIn(rows, url); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
	}
}
