package recognisers

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/rules"
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
	rows := make([]*rules.Recogniser, 0, len(seed))
	for i := range seed {
		rows = append(rows, &seed[i])
	}
	for _, url := range []string{
		"https://github.com/openziti/ziti-console/pull/967",
		"https://github.com/openziti/ziti-console/pull/967/changes",
		"https://github.com/openziti/ziti-console/pull/967/files",
		"https://github.com/openziti/ziti-console/pull/967#issuecomment-1",
	} {
		r, vars, err := rules.MatchRecogniserIn(rows, url)
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
	rows := make([]*rules.Recogniser, 0, len(seed))
	for i := range seed {
		rows = append(rows, &seed[i])
	}
	for _, url := range []string{
		"https://bitbucket.org/acme/widget/pull-requests/12",
		"https://github.com/openziti/ziti/issues/4211",
		"https://netfoundry.zendesk.com/agent/tickets/12345",
		"https://openziti.discourse.group/t/some-topic/4567",
	} {
		if _, _, err := rules.MatchRecogniserIn(rows, url); err != nil {
			t.Fatalf("%s: %v", url, err)
		}
	}
}

// An ssh clone url, with or without .git, fills the same fields as the repository's https url.
func TestTheSeedRecognisesAnSSHCloneURLAsItsRepository(t *testing.T) {
	seed, err := Seed()
	if err != nil {
		t.Fatal(err)
	}
	rows := make([]*rules.Recogniser, 0, len(seed))
	for i := range seed {
		rows = append(rows, &seed[i])
	}
	fill := func(url string) *rules.Resolved {
		r, vars, err := rules.MatchRecogniserIn(rows, url)
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		return r.Fill(vars)
	}
	for https, sshs := range map[string][]string{
		"https://github.com/netfoundry/docusaurus-shared": {"git@github.com:netfoundry/docusaurus-shared.git",
			"git@github.com:netfoundry/docusaurus-shared"},
		"https://gitlab.com/group/repo": {"git@gitlab.com:group/repo.git", "git@gitlab.com:group/repo"},
		"https://bitbucket.org/ws/repo": {"git@bitbucket.org:ws/repo.git", "git@bitbucket.org:ws/repo"},
	} {
		want := fill(https)
		for _, ssh := range sshs {
			got := fill(ssh)
			if got.Host != want.Host || got.Org != want.Org || got.Repo != want.Repo || got.Cwd != want.Cwd ||
				got.Title != want.Title || got.Branch != want.Branch || got.Kind != want.Kind ||
				got.Window != want.Window || strings.Join(got.Tags, ",") != strings.Join(want.Tags, ",") {
				t.Fatalf("%s gave %s %+v, %s gave %s %+v", ssh, got.Recogniser, got, https, want.Recogniser, want)
			}
		}
	}
}
