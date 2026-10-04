package gitsync

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync/atomic"
	"testing"
	"time"
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
	}{{"", "", 0}, {"abc", "abc", 0}, {"abc", "abd", 1}, {"abc", "ab", 1}, {"kitten", "sitting", 3}, {"a", "zzzzzzzz", 8},
		// the same length and far apart: limit+1, and not the distance itself.
		{"abcdefgh", "zzzzzzzz", 8}} {
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
	// A name past the input cap is not looked up, even when the hub knows a repository of that very name.
	long := "github/o/" + strings.Repeat("a", lookupInputMax)
	known[long] = &knownRepo{}
	if got, _ := resolveRepo(long, known); got != "" {
		t.Errorf("a name of %d characters was looked up: %q", len(long), got)
	}
	short := "github/o/" + strings.Repeat("a", 100)
	known[short] = &knownRepo{}
	if got, _ := resolveRepo(short, known); got != short {
		t.Errorf("a name of %d characters was not found: %q", len(short), got)
	}
	delete(known, long)
	delete(known, short)
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

// ── the lookup against rooms that answer from a function ───────────────────────────────────────────────────

// lookupRooms is a hub's rooms, each answering a ref advertisement from a function and counting the asks.
type lookupRooms struct {
	rooms []RoomInfo
	serve func(room string) (string, error)
	asked atomic.Int32
}

func (f *lookupRooms) Attached() []RoomInfo { return f.rooms }
func (f *lookupRooms) Transport(room string) http.RoundTripper {
	return rtFunc(func(r *http.Request) (*http.Response, error) {
		f.asked.Add(1)
		body, err := f.serve(room)
		if err != nil {
			return nil, err
		}
		return &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}, nil
	})
}

func lookupHub(t *testing.T, rooms *lookupRooms, repos ...string) *Hub {
	t.Helper()
	h := &Hub{Dir: t.TempDir(), Rooms: rooms, LookupTTL: time.Hour}
	h.Repos = func() ([]Repo, error) {
		var out []Repo
		for _, r := range repos {
			out = append(out, Repo{Name: r})
		}
		return out, nil
	}
	return h
}

func room(name string) RoomInfo { return RoomInfo{Name: name, Git: true} }

// An advertisement past 4 MiB is not read: the room did not answer, and its branches are not listed.
func TestAnAdvertisementPastTheCapIsNotRead(t *testing.T) {
	big := string(advert(sha1h + " refs/heads/fix/x"))
	big = big[:len(big)-len(flushPkt)] + strings.Repeat(string(pktLine(sha1h+" refs/tags/t\n")), (lookupAdvertMax/48)+100) + string(flushPkt)
	if len(big) <= lookupAdvertMax {
		t.Fatalf("the advertisement is %d, not past the cap", len(big))
	}
	rooms := &lookupRooms{rooms: []RoomInfo{room("sg3")}, serve: func(string) (string, error) { return big, nil }}
	h := lookupHub(t, rooms, "github/o/r")
	a := h.Lookup(context.Background(), URLQuery{Repo: "o/r", Base: "http://h"})
	if len(a.Branches) != 0 || !strings.Contains(a.Note, "sg3 did not answer") {
		t.Fatalf("a room that sent %d bytes was read: %+v", len(big), a)
	}
	// The same advertisement within the cap is read.
	rooms.serve = func(string) (string, error) { return string(advert(sha1h + " refs/heads/fix/x")), nil }
	h = lookupHub(t, rooms, "github/o/r")
	if a := h.Lookup(context.Background(), URLQuery{Repo: "o/r", Base: "http://h"}); len(a.Branches) != 1 {
		t.Fatalf("%+v", a)
	}
}

// Two rooms of 500 branches each do not make an answer of 1000.
func TestAnAnswerIsCutAtFiveHundredBranches(t *testing.T) {
	adv := func(prefix string) string {
		var lines []string
		for i := 0; i < lookupBranchMax; i++ {
			lines = append(lines, fmt.Sprintf("%s refs/heads/%s%04d", sha1h, prefix, i))
		}
		return string(advert(lines...))
	}
	rooms := &lookupRooms{rooms: []RoomInfo{room("sg3"), room("sg4")}, serve: func(r string) (string, error) {
		if r == "sg3" {
			return adv("a"), nil
		}
		return adv("b"), nil
	}}
	h := lookupHub(t, rooms, "github/o/r")
	a := h.Lookup(context.Background(), URLQuery{Repo: "o/r", Base: "http://h"})
	if len(a.Branches) != lookupBranchMax || !strings.Contains(a.Note, "the first 500 branches") {
		t.Fatalf("%d branches, note %q", len(a.Branches), a.Note)
	}
}

// A short name two repositories answer to is not guessed at, and the answer says so and lists them.
func TestAnAmbiguousShortNameIsAnsweredWithTheCandidates(t *testing.T) {
	rooms := &lookupRooms{serve: func(string) (string, error) { return "", errors.New("no room") }}
	h := lookupHub(t, rooms, "github/openziti/zrok", "github/netfoundry/zrok", "github/o/r")
	a := h.Lookup(context.Background(), URLQuery{Repo: "zrok", Base: "http://h"})
	if a.State != URLNotFound || !strings.Contains(a.Note, "more than one repository") || a.Closest == nil ||
		strings.Join(a.Closest.Repos, ",") != "github/netfoundry/zrok,github/openziti/zrok" {
		t.Fatalf("%+v", a)
	}
	if got := a.Text(); !strings.Contains(got, "github/openziti/zrok") {
		t.Fatalf("%s", got)
	}
}

// A room that does not answer is asked again at the next call (it may be back), and not held as down. A room that did
// answer is held.
func TestADownRoomIsAskedAgainAndAnAnsweringRoomIsHeld(t *testing.T) {
	rooms := &lookupRooms{rooms: []RoomInfo{room("sg3")}, serve: func(string) (string, error) { return "", errors.New("down") }}
	h := lookupHub(t, rooms, "github/o/r")
	for i := 0; i < 3; i++ {
		h.Lookup(context.Background(), URLQuery{Repo: "o/r", Base: "http://h"})
	}
	if n := rooms.asked.Load(); n != 3 {
		t.Fatalf("a down room was asked %d times in 3 lookups, want 3", n)
	}
	rooms.asked.Store(0)
	rooms.serve = func(string) (string, error) { return string(advert(sha1h + " refs/heads/fix/x")), nil }
	for i := 0; i < 3; i++ {
		h.Lookup(context.Background(), URLQuery{Repo: "o/r", Base: "http://h"})
	}
	if n := rooms.asked.Load(); n != 1 {
		t.Fatalf("an answering room was asked %d times in 3 lookups, want 1", n)
	}
}

// Only a room the hub saw sync a repository, whole or behind, is counted as having it. A failed, absent or old room
// has none of it to be offline for.
func TestOnlyAnOkOrBehindSyncCountsARoomAsHavingARepo(t *testing.T) {
	h := &Hub{Dir: t.TempDir(), rooms: map[string]*RoomState{}}
	for i, st := range []string{"ok", "behind", "failed", "absent", "unsupported", ""} {
		room := fmt.Sprintf("r%d", i)
		h.rooms[room] = &RoomState{Sync: map[string]SyncResult{"github/o/r": {Room: room, Name: "github/o/r", State: st}}}
	}
	var got []string
	k := h.known(context.Background())["github/o/r"]
	if k == nil {
		t.Fatal("a repository two rooms hold is not known")
	}
	got = append(got, k.rooms...)
	sort.Strings(got)
	if strings.Join(got, ",") != "r0,r1" {
		t.Fatalf("the rooms counted as having it are %v, want r0 and r1", got)
	}
}

// What the caller typed as a room is cut, in the note and in the list.
func TestARoomNamedAtLengthIsCutInTheAnswer(t *testing.T) {
	rooms := &lookupRooms{rooms: []RoomInfo{room("sg3")}, serve: func(string) (string, error) { return "", errors.New("x") }}
	h := lookupHub(t, rooms, "github/o/r")
	long := strings.Repeat("r", 5000)
	a := h.Lookup(context.Background(), URLQuery{Repo: "o/r", Room: long, Branch: long, Base: "http://h"})
	if a.State != URLOffline || len(a.Note) > 400 || len(a.Offline) != 1 || len(a.Offline[0]) > 100 || len(a.Branch) > 100 {
		t.Fatalf("%d %d %+v", len(a.Note), len(a.Branch), a.Offline)
	}
	// A branch that is not there is cut too, in the miss.
	b := h.Lookup(context.Background(), URLQuery{Repo: "o/r", Branch: long, Base: "http://h"})
	if b.State != URLNotFound || len(b.Note) > 400 || len(b.Branch) > 100 {
		t.Fatalf("%d %d", len(b.Note), len(b.Branch))
	}
}

// What a card is given: the hub's URLs on its room's forwarder, none for a room's work, and the original untouched.
func TestForCardRewritesTheHubsURLsAndTakesTheRoomsAway(t *testing.T) {
	a := URLAnswer{State: URLFound, Repo: "github/o/r", Branch: "fix/x", Branches: []URLBranch{{Name: "fix/x", Sources: []URLSource{
		{Source: "hub", URL: "http://127.0.0.1:7778/git/hub/github/o/r.git", SHA: sha1h},
		{Source: "room", Room: "sg4", Online: true, URL: "http://127.0.0.1:7778/git/room/sg4/github/o/r.git", SHA: sha2h, Ahead: ptr(true)},
	}}}}
	c := a.ForCard("http://127.0.0.1:7777/git/")
	hub, rm := c.Branches[0].Sources[0], c.Branches[0].Sources[1]
	if hub.URL != "http://127.0.0.1:7777/git/hub/github/o/r.git" || rm.URL != "" || rm.Note != NoRoomForCards || rm.Ahead == nil || !*rm.Ahead {
		t.Fatalf("%+v %+v", hub, rm)
	}
	if a.Branches[0].Sources[0].URL != "http://127.0.0.1:7778/git/hub/github/o/r.git" || a.Branches[0].Sources[1].URL == "" {
		t.Fatal("the answer it was made from was changed")
	}
	// The line gives the hub's, even with the room ahead, and says why the room is left.
	if got := c.Text(); !strings.HasPrefix(got, "fetch it with: git fetch http://127.0.0.1:7777/git/hub/github/o/r.git fix/x") ||
		!strings.Contains(got, "atrium_git_push") {
		t.Fatalf("%s", got)
	}
	// No forwarder said: nothing is given, and the line says so and does not tell a card to fetch.
	n := a.ForCard("")
	if s := n.Branches[0].Sources[0]; s.URL != "" || s.Note != NoHubRemote {
		t.Fatalf("%+v", s)
	}
	if got := n.Text(); strings.Contains(got, "git fetch") {
		t.Fatalf("%s", got)
	}
	// A room's branch alone has no URL and says what to do.
	only := URLAnswer{State: URLFound, Branch: "b", Branches: []URLBranch{{Name: "b", Sources: []URLSource{
		{Source: "room", Room: "sg4", Online: true, URL: "http://h/git/room/sg4/x.git"}}}}}.ForCard("http://127.0.0.1:7777/git/")
	if got := only.Text(); strings.Contains(got, "git fetch") || !strings.Contains(got, "atrium_git_push") {
		t.Fatalf("%s", got)
	}
}
