//go:build integration

package link

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/forge"
	"github.com/dovholuknf/atrium/internal/linkfetch"
	"github.com/dovholuknf/atrium/internal/store"
)

// discourseRedirects stands in for a Discourse forum: /t/<slug> redirects to /t/<slug>/<num>, as the real one does,
// for the slugs in topics, and anything else is a 404.
func discourseRedirects(t *testing.T, topics map[string]string) {
	t.Helper()
	was := linkfetch.Client
	linkfetch.Client = &http.Client{Transport: roundTrip(func(r *http.Request) (*http.Response, error) {
		res := &http.Response{Request: r, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(""))}
		slug := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/t/"), "/")
		switch num, ok := topics[slug]; {
		case ok:
			res.StatusCode, res.Status = http.StatusMovedPermanently, "301 Moved Permanently"
			res.Header.Set("Location", "https://"+r.URL.Host+"/t/"+slug+"/"+num)
		case strings.Count(r.URL.Path, "/") == 3:
			res.StatusCode, res.Status = http.StatusOK, "200 OK"
		default:
			res.StatusCode, res.Status = http.StatusNotFound, "404 Not Found"
		}
		return res, nil
	})}
	t.Cleanup(func() { linkfetch.Client = was })
}

// recogniseVia is a POST /v1/recognise through the hub, with a room named or not.
func (x *claimHub) recogniseVia(t *testing.T, room, url string) (int, store.Resolved) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"url": url})
	req, _ := http.NewRequest(http.MethodPost, x.front.URL+"/v1/recognise", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(RoomHeader, room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out store.Resolved
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
	}
	return res.StatusCode, out
}

// gwtHub is a hub on the built-in rows with two rooms, the least busy of them on a build that reads no hub rows, a
// forge that answers every pull request with branch fix-branch, and a Discourse forum that redirects one slug.
func gwtHub(t *testing.T) *claimHub {
	t.Helper()
	x := newClaimHub(t, map[string]int{"current": 2, "old": 0})
	x.rooms["old"].mu.Lock()
	x.rooms["old"].deaf = true
	x.rooms["old"].mu.Unlock()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, func(context.Context, forge.Cmd) ([]byte, error) {
		return nil, errors.New("no CLI in a test")
	})
	ff := &fakeForge{kind: forge.GitHub, pr: forge.PR{Title: "a fix", HeadRef: "fix-branch", BaseRef: "main"}}
	x.proxy.forgeSide().forgeOf = func(string) (forge.Forge, error) { return ff, nil }
	discourseRedirects(t, map[string]string{
		"mfa-posture-check-keeps-failing-after-successful-mfa-enrollment-auth-an-mfa-enrollment-already": "5123",
	})
	return x
}

const mfaTopic = "https://openziti.discourse.group/t/mfa-posture-check-keeps-failing-after-successful-mfa-enrollment-auth-an-mfa-enrollment-already"

// EVERY LINK GWT OPENS, ATRIUM OPENS TO THE SAME PLACE (r-recognisers-gwt). One row per url shape gwt's dispatch
// takes, in dotfiles/powershell/onpath/git-worktree.ps1: the recogniser that answers, the directory and the branch
// gwt would have made. Recognised by the hub with no room named, the way ctrl-alt-r asks.
func TestEveryLinkGwtOpensIsRecognisedToTheSamePlace(t *testing.T) {
	x := gwtHub(t)
	defer x.done()

	const wt, src = "D:/worktrees/", "D:/git/"
	for _, c := range []struct{ url, row, cwd, branch string }{
		// gwt pr: <host>/<org>/<repo>/pull/<num>, a trailing tab stripped. The branch is the PR's head.
		{"https://github.com/openziti/ziti/pull/4211", "github-pull-request", wt + "github/openziti/ziti/pr-4211", "fix-branch"},
		{"https://github.com/openziti/ziti/pull/4211/files", "github-pull-request", wt + "github/openziti/ziti/pr-4211", "fix-branch"},
		{"https://github.com/openziti/ziti/pull/4211/changes?w=1", "github-pull-request", wt + "github/openziti/ziti/pr-4211", "fix-branch"},
		{"https://github.com/openziti/ziti/pull/4211#issuecomment-2216400977", "github-pull-request", wt + "github/openziti/ziti/pr-4211", "fix-branch"},
		// gwt pr, bitbucket: pull-requests, a trailing /diff, /commits or /activity stripped.
		{"https://bitbucket.org/netfoundry/ziti-fabric/pull-requests/77/diff", "bitbucket-pull-request", wt + "bitbucket/netfoundry/ziti-fabric/pr-77", "fix-branch"},
		{"https://bitbucket.org/netfoundry/ziti-fabric/pull-requests/77?tab=activity", "bitbucket-pull-request", wt + "bitbucket/netfoundry/ziti-fabric/pr-77", "fix-branch"},
		// gwt issue: <host>/<org>/<repo>/issues/<num> on any host, branch issue-<num>.
		{"https://github.com/openziti/ziti/issues/3120", "github-issue", wt + "github/openziti/ziti/issue-3120", "issue-3120"},
		{"https://github.com/openziti/ziti/issues/3120#issuecomment-1", "github-issue", wt + "github/openziti/ziti/issue-3120", "issue-3120"},
		{"https://bitbucket.org/netfoundry/widgets/issues/12/the-title-bitbucket-adds", "bitbucket-issue", wt + "bitbucket/netfoundry/widgets/issue-12", "issue-12"},
		{"https://gitlab.com/acme/widgets/issues/5", "gitlab-issue", wt + "gitlab/acme/widgets/issue-5", "issue-5"},
		{"https://gitlab.com/acme/widgets/-/issues/5?sort=asc", "gitlab-issue", wt + "gitlab/acme/widgets/issue-5", "issue-5"},
		// gwt advisory: .../security/advisories/GHSA-..., branch advisory-<GHSA id>.
		{"https://github.com/openziti/ziti/security/advisories/GHSA-p6gx-g438-rjc8", "github-advisory", wt + "github/openziti/ziti/advisory-GHSA-p6gx-g438-rjc8", "advisory-GHSA-p6gx-g438-rjc8"},
		// gwt ghsa: the advisory's temporary fork, on the base repo's clone. Tree: advisory-<id> on the fix branch.
		{"https://github.com/openziti/ziti-ghsa-p6gx-g438-rjc8/tree/fix-the-thing", "github-advisory-fork-branch", wt + "github/openziti/ziti/advisory-p6gx-g438-rjc8", "fix-the-thing"},
		// PR: advisory-<id>-pr<num>.
		{"https://github.com/openziti/ziti-ghsa-p6gx-g438-rjc8/pull/1/files", "github-advisory-fork-pr", wt + "github/openziti/ziti/advisory-p6gx-g438-rjc8-pr1", "advisory-p6gx-g438-rjc8-pr1"},
		// gwt zendesk: any <sub>.zendesk.com, anything before /tickets/<num>, in openziti/ziti-tunnel-sdk-c.
		{"https://netfoundry.zendesk.com/agent/tickets/15925", "zendesk-ticket", wt + "github/openziti/ziti-tunnel-sdk-c/zendesk-15925", "zendesk-15925"},
		{"https://netfoundry.zendesk.com/agent/tickets/15925?brand_id=1#comment", "zendesk-ticket", wt + "github/openziti/ziti-tunnel-sdk-c/zendesk-15925", "zendesk-15925"},
		{"https://acme.zendesk.com/tickets/42", "zendesk-ticket", wt + "github/openziti/ziti-tunnel-sdk-c/zendesk-42", "zendesk-42"},
		// gwt discourse: /t/<slug>/<id>, a trailing post number, /t/<id>, in openziti/ziti.
		{mfaTopic + "/5123", "discourse-topic", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		{mfaTopic + "/5123/7", "discourse-topic", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		{mfaTopic + "/5123?u=clint#post_3", "discourse-topic", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		{"https://openziti.discourse.group/t/5123", "discourse-topic", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		// gwt discourse with no id: the forum redirects /t/<slug> to /t/<slug>/<id>, and the id is read off that.
		{mfaTopic, "discourse-topic-slug", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		{mfaTopic + "/", "discourse-topic-slug", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		{mfaTopic + "?u=clint", "discourse-topic-slug", wt + "github/openziti/ziti/discourse-5123", "discourse-5123"},
		// gwt clone: a bare repo url, on the main clone, a .git dropped.
		{"https://github.com/openziti/ziti", "github-repo", src + "github/openziti/ziti", ""},
		{"https://github.com/openziti/ziti.git", "github-repo", src + "github/openziti/ziti", ""},
		{"https://bitbucket.org/netfoundry/widgets/", "bitbucket-repo", src + "bitbucket/netfoundry/widgets", ""},
		{"https://gitlab.com/acme/widgets.git", "gitlab-repo", src + "gitlab/acme/widgets", ""},
		// gwt new: a deeper url on a known host. gwt asks for a name, and a branch page names its own.
		{"https://github.com/openziti/ziti/tree/release-v1.6", "github-branch", wt + "github/openziti/ziti/release-v1.6", "release-v1.6"},
	} {
		code, got := x.recogniseVia(t, "", c.url)
		if code != http.StatusOK {
			t.Errorf("%s answered %d", c.url, code)
			continue
		}
		if got.Recogniser != c.row || got.Cwd != c.cwd || got.Branch != c.branch {
			t.Errorf("%s\n got %s %q %q (%s)\nwant %s %q %q", c.url, got.Recogniser, got.Cwd, got.Branch,
				got.FetchError, c.row, c.cwd, c.branch)
		}
	}
	if hits := x.rooms["old"].refused(); hits != 0 {
		t.Errorf("a recognise reached a room: %d", hits)
	}
}

// A slug-only Discourse link the forum does not redirect still opens the dialog, with the hole in the directory and
// the reason beside it.
func TestASlugOnlyTopicThatDoesNotRedirectSaysWhy(t *testing.T) {
	x := gwtHub(t)
	defer x.done()
	code, got := x.recogniseVia(t, "", "https://openziti.discourse.group/t/no-such-topic")
	if code != http.StatusOK || got.Recogniser != "discourse-topic-slug" {
		t.Fatalf("answered %d %+v", code, got)
	}
	if got.Cwd != "D:/worktrees/github/openziti/ziti/discourse-{num}" || got.FetchError == "" ||
		!strings.Contains(got.Problem, "{num}") {
		t.Fatalf("the hole and the reason: %+v", got)
	}
}

// THE BUG (r-recognise-placed-on-control-room): with two rooms and none named, a paste went to the least busy room,
// which ran a build that reads no hub rows and answered 404. The hub answers now, whichever room is least busy and
// whatever it runs. A link no row matches is still a 404.
func TestRecogniseWithNoRoomIsAnsweredWhenTheLeastBusyRoomIsOld(t *testing.T) {
	x := gwtHub(t)
	defer x.done()
	code, got := x.recogniseVia(t, "", mfaTopic+"/5123")
	if code != http.StatusOK || got.Recogniser != "discourse-topic" || got.Vars["num"] != "5123" {
		t.Fatalf("no room named = %d %+v", code, got)
	}
	if code, _ := x.recogniseVia(t, "", "https://example.com/not/a/thing"); code != http.StatusNotFound {
		t.Fatalf("a link nothing matches = %d", code)
	}
	// A room named still answers for itself, old as it is: the claimRoom has no recognise route.
	if code, _ := x.recogniseVia(t, "old", mfaTopic+"/5123"); code != http.StatusNotFound {
		t.Fatalf("the old room named = %d", code)
	}
}

// AN OPEN PLACED ON A ROOM TOO OLD TO READ THE HUB'S ROWS GOES ON TO THE NEXT, and the old room is passed over after
// that, while it runs the same build. A link the hub's rows do not match is the room's honest answer and is not retried.
func TestAnOpenPlacedOnAnOldRoomGoesToTheNext(t *testing.T) {
	x := gwtHub(t)
	defer x.done()

	code, placed, out := x.openVia(t, "", mfaTopic+"/5123")
	if code != http.StatusCreated || placed != "current" || out["room"] != "current" || out["card"] != "current~c1" {
		t.Fatalf("open = %d %q %v", code, placed, out)
	}
	if hits := x.rooms["old"].refused(); hits != 1 {
		t.Fatalf("the old room was asked %d times, wanted 1", hits)
	}
	code, placed, _ = x.openVia(t, "", "https://netfoundry.zendesk.com/agent/tickets/15925")
	if code != http.StatusCreated || placed != "current" {
		t.Fatalf("second open = %d %q", code, placed)
	}
	if hits := x.rooms["old"].refused(); hits != 1 {
		t.Fatalf("the old room was asked again: %d", hits)
	}
	if got := x.rooms["current"].opened(); len(got) != 2 {
		t.Fatalf("current opened %v", got)
	}
}

// A paste of a pull request with no room named is retried the same way.
func TestAPastePlacedOnAnOldRoomGoesToTheNext(t *testing.T) {
	x := gwtHub(t)
	defer x.done()
	x.rooms["current"].recognise = true
	code, body := x.pasteVia(t, "", "https://github.com/openziti/ziti/pull/4211")
	if code != http.StatusCreated {
		t.Fatalf("paste = %d %s", code, body)
	}
	if got := x.rooms["current"].made(); len(got) != 1 {
		t.Fatalf("current made %v", got)
	}
	if hits := x.rooms["old"].refused(); hits != 1 {
		t.Fatalf("the old room was asked %d times, wanted 1", hits)
	}
}
