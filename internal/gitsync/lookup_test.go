package gitsync

import (
	"strings"
	"testing"
)

const sha1h = "1111111111111111111111111111111111111111"
const sha2h = "2222222222222222222222222222222222222222"

func advert(lines ...string) []byte {
	out := pktLine("# service=git-upload-pack\n")
	out = append(out, flushPkt...)
	for i, l := range lines {
		if i == 0 {
			l += "\x00multi_ack side-band-64k symref=HEAD:refs/heads/x"
		}
		out = append(out, pktLine(l+"\n")...)
	}
	return append(out, flushPkt...)
}

func TestParseAdvertTakesServedBranchesAndNothingElse(t *testing.T) {
	got, ok := parseAdvert(advert(
		sha1h+" refs/heads/claude/w1",
		sha2h+" refs/heads/fix/live",
		sha1h+" refs/heads/main",
		sha1h+" refs/heads/master",
		sha1h+" refs/heads/hub-main",
		sha1h+" refs/heads/claude/main",
		sha1h+" refs/stash",
		sha1h+" refs/notes/commits",
		sha1h+" refs/tags/v1",
		sha1h+" refs/tags/v1^{}",
		sha1h+" HEAD",
		"zz refs/heads/bad",
	))
	if !ok {
		t.Fatal("not read")
	}
	if len(got) != 2 || got["claude/w1"] != sha1h || got["fix/live"] != sha2h {
		t.Fatalf("%v", got)
	}
}

func TestParseAdvertRefusesWhatIsNotOne(t *testing.T) {
	for name, b := range map[string][]byte{
		"empty":     nil,
		"html":      []byte("<html>not found</html>"),
		"an ERR":    append(append(pktLine("# service=git-upload-pack\n"), flushPkt...), pktLine("ERR atrium: no\n")...),
		"no flush":  pktLine("# service=git-upload-pack\n"),
		"not a svc": pktLine("hello\n"),
	} {
		if _, ok := parseAdvert(b); ok {
			t.Errorf("%s was taken", name)
		}
	}
	// An advertisement of no refs, which an empty repository sends, is read and empty.
	got, ok := parseAdvert(advert(sha1h + " capabilities^{}"))
	if !ok || len(got) != 0 {
		t.Errorf("%v %v", got, ok)
	}
}

func TestParseAdvertIsBounded(t *testing.T) {
	var lines []string
	for i := 0; i < lookupBranchMax+50; i++ {
		lines = append(lines, sha1h+" refs/heads/b"+strings.Repeat("x", i%7)+string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('a'+i/676)))
	}
	got, ok := parseAdvert(advert(lines...))
	if !ok || len(got) > lookupBranchMax {
		t.Fatalf("%d %v", len(got), ok)
	}
}

func TestClosestNamesAreNearAndBounded(t *testing.T) {
	have := []string{"github/openziti/zrok", "github/openziti/ziti", "github/netfoundry/zrok-docs", "github/o/r",
		"gitlab/a/zrok", "github/x/y1", "github/x/y2", "github/x/y3", "github/x/y4"}
	for _, c := range []struct {
		want string
		in   string
	}{
		{"zrok", "github/openziti/zrok"},
		{"openziti/zrk", "github/openziti/zrok"},
		{"github/openziti/zroc", "github/openziti/zrok"},
		{"ziti", "github/openziti/ziti"},
	} {
		got := closestNames(c.want, have, LookupClosestMax)
		found := false
		for _, g := range got {
			found = found || g == c.in
		}
		if !found || len(got) > LookupClosestMax {
			t.Errorf("%q -> %v, want %s", c.want, got, c.in)
		}
	}
	if got := closestNames("y", have, 3); len(got) != 3 {
		t.Errorf("not bounded: %v", got)
	}
	if got := closestNames("qqqqqqqq", have, 5); len(got) != 0 {
		t.Errorf("nothing is close: %v", got)
	}
	if got := closestNames("", have, 5); got != nil {
		t.Errorf("%v", got)
	}
	// A long thing typed is not compared at length.
	if got := closestNames(strings.Repeat("a", 5000), have, 5); got != nil {
		t.Errorf("%v", got)
	}
}

func TestEditDistance(t *testing.T) {
	for _, c := range []struct {
		a, b string
		d    int
	}{{"", "", 0}, {"abc", "abc", 0}, {"abc", "abd", 1}, {"abc", "ab", 1}, {"kitten", "sitting", 3}, {"a", "zzzzzzzz", 8}} {
		want := c.d
		if want > 3 {
			want = 4 // over the limit is limit+1
		}
		if got := editDistance(c.a, c.b, 3); got != want {
			t.Errorf("%q %q = %d want %d", c.a, c.b, got, want)
		}
	}
}

func TestResolveRepoFindsOnlyWhatTheHubKnows(t *testing.T) {
	known := map[string]*knownRepo{
		"github/openziti/zrok": {}, "github/netfoundry/zrok": {}, "github/o/r": {}, "gitlab/a/b": {},
	}
	for in, want := range map[string]string{
		"github/o/r":                       "github/o/r",
		"o/r":                              "github/o/r",
		"O/R":                              "github/o/r",
		"r":                                "github/o/r",
		"https://github.com/o/r.git":       "github/o/r",
		"git@github.com:openziti/zrok.git": "github/openziti/zrok",
		"gitlab/a/b":                       "gitlab/a/b",
		"b":                                "gitlab/a/b",
		"github/o/nope":                    "",
		"../../etc":                        "",
		"https://u:p@github.com/o/r":       "",
		"":                                 "",
		strings.Repeat("a/", 400):          "",
	} {
		if got, _ := resolveRepo(in, known); got != want {
			t.Errorf("%q -> %q want %q", in, got, want)
		}
	}
	// A short name two repositories answer to is not guessed.
	got, cands := resolveRepo("zrok", known)
	if got != "" || len(cands) != 2 {
		t.Errorf("%q %v", got, cands)
	}
}

func TestTheLineAModelReads(t *testing.T) {
	both := URLAnswer{State: URLFound, Repo: "github/o/r", Branch: "fix/x", Branches: []URLBranch{{Name: "fix/x", Sources: []URLSource{
		{Source: "hub", URL: "http://h/git/hub/github/o/r.git", SHA: sha1h},
		{Source: "room", Room: "sg4", Online: true, URL: "http://h/git/room/sg4/github/o/r.git", SHA: sha2h, Ahead: ptr(true)},
	}}}}
	if got := both.Text(); !strings.HasPrefix(got, "fetch it with: git fetch http://h/git/room/sg4/github/o/r.git fix/x") {
		t.Errorf("room ahead: %s", got)
	}
	both.Branches[0].Sources[1].Ahead = ptr(false)
	if got := both.Text(); !strings.HasPrefix(got, "fetch it with: git fetch http://h/git/hub/github/o/r.git fix/x") {
		t.Errorf("room not ahead: %s", got)
	}
	roomOnly := URLAnswer{State: URLFound, Branch: "b", Branches: []URLBranch{{Name: "b", Sources: []URLSource{
		{Source: "room", Room: "sg4", URL: "http://h/git/room/sg4/x.git"}}}}}
	if got := roomOnly.Text(); !strings.Contains(got, "git fetch http://h/git/room/sg4/x.git b") {
		t.Errorf("%s", got)
	}
	miss := URLAnswer{State: URLNotFound, Note: "no", Closest: &URLClosest{Repos: []string{"a", "b"}, Branches: []string{"c"}}}
	if got := miss.Text(); got != "not found: no. closest repositories: a, b. closest branches: c" {
		t.Errorf("%s", got)
	}
	if got := (URLAnswer{State: URLOffline, Note: "sg4 is not connected"}).Text(); got != "offline: sg4 is not connected" {
		t.Errorf("%s", got)
	}
}

func ptr[T any](v T) *T { return &v }
