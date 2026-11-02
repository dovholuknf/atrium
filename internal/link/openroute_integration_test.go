//go:build integration

package link

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/link/wire"
)

func (c *claimRoom) opened() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]string(nil), c.opens...)
}

// openVia is a POST /v1/open through the hub, with a room named or not. It answers the status, the placed room
// header and the body.
func (x *claimHub) openVia(t *testing.T, room, url string) (int, string, map[string]any) {
	t.Helper()
	req, _ := http.NewRequest(http.MethodPost, x.front.URL+"/v1/open", strings.NewReader(`{"url":"`+url+`"}`))
	req.Header.Set("Content-Type", "application/json")
	if room != "" {
		req.Header.Set(wire.RoomHeader, room)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return res.StatusCode, res.Header.Get(PlacedRoomHeader), out
}

// AN OPEN GOES TO THE ROOM THAT HOLDS THE LINK, whatever its load, else to the least busy room, and a named room
// wins over both. The answer carries the room, and the card and row ids tagged with it.
func TestOpenOnTheHubGoesToTheHolderElseTheLeastBusyRoom(t *testing.T) {
	pr := seedRow("github-pull-request", 10,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/pull/(?P<num>\d+)(?:[/?#].*)?$`)
	pr.Tags = "pull-request,{repo}"
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	seedHub(x, pr)

	// nobody holds it: the least busy room
	code, placed, out := x.openVia(t, "", "https://github.com/openziti/zrok/pull/11")
	if code != http.StatusCreated || placed != "beta" || out["room"] != "beta" || out["card"] != "beta~c1" ||
		out["pr"] != "beta~pr_1" {
		t.Fatalf("unheld = %d %q %v", code, placed, out)
	}
	if got := x.rooms["beta"].opened(); len(got) != 1 {
		t.Fatalf("beta opened %v", got)
	}

	// alpha holds it: alpha, busy as it is
	if _, _, err := x.st.ClaimPR("github.com/openziti/zrok/12", "alpha", "paste"); err != nil {
		t.Fatal(err)
	}
	code, placed, out = x.openVia(t, "", "https://github.com/openziti/zrok/pull/12/files")
	if code != http.StatusCreated || placed != "alpha" || out["card"] != "alpha~c1" {
		t.Fatalf("held = %d %q %v", code, placed, out)
	}

	// a named room wins
	code, _, out = x.openVia(t, "alpha", "https://github.com/openziti/zrok/pull/13")
	if code != http.StatusCreated || out["room"] != "alpha" || out["card"] != "alpha~c1" {
		t.Fatalf("named = %d %v", code, out)
	}
	if got := x.rooms["alpha"].opened(); len(got) != 2 {
		t.Fatalf("alpha opened %v", got)
	}
}

// A PASTE GOES TO A ROOM THAT ALREADY HOLDS THE REPO, busy as it is, so the repo is not cloned on another room. A room
// that holds it and is not asked (an older build, which answers 404) counts as one that does not.
func TestOpenOnTheHubPrefersARoomThatHoldsTheRepo(t *testing.T) {
	pr := seedRow("github-pull-request", 10,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/pull/(?P<num>\d+)(?:[/?#].*)?$`)
	pr.Tags = "pull-request,{repo}"
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	seedHub(x, pr)
	x.rooms["alpha"].holds = map[string]bool{"github.com/openziti/ziti-console": true}
	x.rooms["beta"].holds = map[string]bool{}

	_, placed, _ := x.openVia(t, "", "https://github.com/openziti/ziti-console/pull/960/changes")
	if placed != "alpha" {
		t.Fatalf("a repo alpha holds went to %q", placed)
	}
	// a repo nobody holds: the least busy room, as before
	_, placed, _ = x.openVia(t, "", "https://github.com/openziti/zrok/pull/5")
	if placed != "beta" {
		t.Fatalf("a repo nobody holds went to %q", placed)
	}
	// rooms that cannot say are placed by load alone
	x.rooms["alpha"].holds, x.rooms["beta"].holds = nil, nil
	_, placed, _ = x.openVia(t, "", "https://github.com/openziti/ziti-console/pull/961")
	if placed != "beta" {
		t.Fatalf("rooms that cannot say went to %q", placed)
	}
}

// AN ISSUE OR A BRANCH LINK IS CLAIMED BY ITS KEY, as a PR's is. A re-paste goes to the room that holds the claim, busy
// as it is, and a first paste with no claim goes to the least busy room. A PR link still routes as it did.
func TestOpenOnTheHubRoutesARepastedIssueOrBranchToItsClaim(t *testing.T) {
	pr := seedRow("github-pull-request", 10,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/pull/(?P<num>\d+)(?:[/?#].*)?$`)
	pr.Tags = "pull-request,{repo}"
	issue := seedRow("github-issue", 20,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/issues/(?P<num>\d+)(?:[/?#].*)?$`)
	issue.Tags, issue.Branch, issue.Kind = "issue", "issue-{num}", "github"
	branch := seedRow("github-branch", 30,
		`^https?://(?P<host>github\.com)/(?P<org>[A-Za-z0-9_.-]+)/(?P<repo>[A-Za-z0-9_.-]+)/tree/(?P<ref>[A-Za-z0-9_./-]+)$`)
	branch.Tags, branch.Branch, branch.Kind = "branch", "{ref}", "github"
	x := newClaimHub(t, map[string]int{"alpha": 3, "beta": 0})
	defer x.done()
	x.proxy.SetForge(&memSettings{m: map[string]string{}}, nil)
	seedHub(x, pr, issue, branch)

	for _, c := range []struct{ key, url string }{
		{"github.com/openziti/zrok/i12", "https://github.com/openziti/zrok/issues/12"},
		{"github.com/openziti/zrok@feat/y", "https://github.com/openziti/zrok/tree/feat/y"},
		{"github.com/openziti/zrok/12", "https://github.com/openziti/zrok/pull/12/files"},
		// a room claims the lower-case key, so a link pasted in mixed case routes to it on the second paste
		{"github.com/openziti/zrok/i15", "https://github.com/OpenZiti/Zrok/issues/15"},
	} {
		if _, _, err := x.st.ClaimPR(c.key, "alpha", "open"); err != nil {
			t.Fatal(err)
		}
		code, placed, out := x.openVia(t, "", c.url)
		if code != http.StatusCreated || placed != "alpha" || out["room"] != "alpha" {
			t.Errorf("%s held by alpha = %d %q %v", c.url, code, placed, out)
		}
	}

	// no claim: the least busy room. The room claims the key when it opens, so the hub does not
	_, placed, _ := x.openVia(t, "", "https://github.com/openziti/zrok/issues/13")
	if placed != "beta" {
		t.Errorf("an unclaimed issue went to %q", placed)
	}
	if _, err := x.st.PRClaimOf("github.com/openziti/zrok/i13"); err == nil {
		t.Error("the hub claimed a key the room has not")
	}
	// the claim table keeps keys in lower case, so a branch pasted in another case still finds its claim
	if _, _, err := x.st.ClaimPR("github.com/openziti/zrok@feat/z", "alpha", "open"); err != nil {
		t.Fatal(err)
	}
	_, placed, _ = x.openVia(t, "", "https://github.com/openziti/zrok/tree/Feat/Z")
	if placed != "alpha" {
		t.Errorf("a branch in another case went to %q", placed)
	}
}
