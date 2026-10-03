package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/edge"
	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// The lookup a card calls for the URL to fetch code it does not have, over a real link: the pass-through rig (a hub, a
// room "SG3" that serves git from a real clone) with a hub STORE beside it that holds finished work. See
// docs/rnd/hub-forge-design.md section 4.

// urlRig is the pass-through rig with a hub store for github/o/r, seeded with a main.
type urlRig struct {
	*passRig
	store string
}

func newURLRig(t *testing.T) *urlRig {
	t.Helper()
	x := &urlRig{passRig: newPassRig(t)}
	// The store is somewhere other than the mirror the rig made under the hub's dir, which is git_repos' and not a
	// store repository.
	x.store = filepath.Join(x.root, "store")
	x.g.StoreRoot = func() string { return x.store }
	// A forge for the one seed of main.
	forge := filepath.Join(x.root, "forge.git")
	gitRun(t, "", "init", "-q", "--bare", "-b", "master", forge)
	work := filepath.Join(x.root, "forgework")
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "init", "-q", "-b", "master")
	if err := os.WriteFile(filepath.Join(work, "m.txt"), []byte("main"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "add", "-A")
	gitRun(t, work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", "main")
	gitRun(t, work, "push", "-q", forge, "HEAD:refs/heads/master")
	x.g.Store().Protocols = "file"
	x.g.Store().Forge = func(gitsync.Ref) string { return forge }
	if _, err := x.g.Store().Init(context.Background(), "https://github.com/o/r"); err != nil {
		t.Fatal(err)
	}
	// Each lookup asks the room afresh, except in the test of the hold.
	x.g.LookupTTL = time.Nanosecond
	return x
}

// pushToStore puts a commit of the room's clone in the hub's store as a branch, the way finished work gets there.
func (x *urlRig) pushToStore(t *testing.T, sha, branch string) {
	t.Helper()
	gitRun(t, x.clone, "push", "-q", filepath.Join(x.store, "github", "o", "r.git"), sha+":refs/heads/"+branch)
}

func (x *urlRig) commit(t *testing.T, branch, file string) string {
	t.Helper()
	gitRun(t, x.clone, "checkout", "-q", branch)
	if err := os.WriteFile(filepath.Join(x.clone, file), []byte(file), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, x.clone, "add", "-A")
	gitRun(t, x.clone, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false", "commit", "-q", "-m", file)
	return gitRun(t, x.clone, "rev-parse", "HEAD")
}

// ask is GET /_hub/git/url from loopback, and the decoded answer.
func (x *urlRig) ask(t *testing.T, query string) gitsync.URLAnswer {
	t.Helper()
	code, body := x.get(t, x.proxy, "127.0.0.1:7778", "127.0.0.1:5555", query)
	if code != http.StatusOK {
		t.Fatalf("GET /_hub/git/url?%s = %d %s", query, code, body)
	}
	var a gitsync.URLAnswer
	if err := json.Unmarshal([]byte(body), &a); err != nil {
		t.Fatalf("%v: %s", err, body)
	}
	return a
}

func (x *urlRig) get(t *testing.T, h http.Handler, host, remote, query string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "http://"+host+"/_hub/git/url?"+query, nil)
	req.RemoteAddr = remote
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec.Code, strings.TrimSpace(rec.Body.String())
}

func branchOf(a gitsync.URLAnswer, name string) *gitsync.URLBranch {
	for i := range a.Branches {
		if a.Branches[i].Name == name {
			return &a.Branches[i]
		}
	}
	return nil
}

func sourceOf(b *gitsync.URLBranch, source string) *gitsync.URLSource {
	if b == nil {
		return nil
	}
	for i := range b.Sources {
		if b.Sources[i].Source == source {
			return &b.Sources[i]
		}
	}
	return nil
}

func TestABranchOnTheHubAloneAnswersTheHubsURL(t *testing.T) {
	x := newURLRig(t)
	x.pushToStore(t, x.claudeW1, "feat/done")
	a := x.ask(t, "repo=o/r&branch=feat/done")
	if a.State != gitsync.URLFound || a.Repo != "github/o/r" || len(a.Branches) != 1 {
		t.Fatalf("%+v", a)
	}
	b := &a.Branches[0]
	if len(b.Sources) != 1 {
		t.Fatalf("sources: %+v", b.Sources)
	}
	hub := sourceOf(b, "hub")
	if hub == nil || hub.SHA != x.claudeW1 || hub.Room != "" || hub.Ahead != nil ||
		!strings.HasSuffix(hub.URL, "/git/hub/github/o/r.git") {
		t.Fatalf("hub source: %+v", hub)
	}
}

func TestABranchOnAnOnlineRoomAloneAnswersTheRoomsURL(t *testing.T) {
	x := newURLRig(t)
	a := x.ask(t, "repo=github/o/r&branch=claude/w1")
	if a.State != gitsync.URLFound || len(a.Branches) != 1 || len(a.Branches[0].Sources) != 1 {
		t.Fatalf("%+v", a)
	}
	room := sourceOf(&a.Branches[0], "room")
	if room == nil || room.Room != "SG3" || !room.Online || room.SHA != x.claudeW1 || room.Ahead != nil ||
		!strings.HasSuffix(room.URL, "/git/room/SG3/github/o/r.git") {
		t.Fatalf("room source: %+v", room)
	}
	// And the URL it gives is one git can fetch from.
	dst := reader(t)
	run(t, dst, "fetch", "-q", strings.Replace(room.URL, "http://127.0.0.1:7778", x.srv.URL, 1), "claude/w1")
	if got := run(t, dst, "rev-parse", "FETCH_HEAD"); got != x.claudeW1 {
		t.Fatalf("fetched %s want %s", got, x.claudeW1)
	}
}

func TestABranchOnBothAnswersBothAndSaysWhetherTheRoomIsAhead(t *testing.T) {
	x := newURLRig(t)
	x.setLive("fix/live")

	// Equal: the same tip on both. Not ahead.
	x.pushToStore(t, x.liveSHA, "fix/live")
	b := branchOf(x.ask(t, "repo=o/r"), "fix/live")
	hub, room := sourceOf(b, "hub"), sourceOf(b, "room")
	if hub == nil || room == nil || room.Ahead == nil || *room.Ahead || hub.SHA != x.liveSHA || room.SHA != x.liveSHA {
		t.Fatalf("equal: %+v", b)
	}

	// The room commits again: it has a commit the hub does not.
	newer := x.commit(t, "fix/live", "more.txt")
	b = branchOf(x.ask(t, "repo=o/r&branch=fix/live"), "fix/live")
	hub, room = sourceOf(b, "hub"), sourceOf(b, "room")
	if hub == nil || room == nil || room.Ahead == nil || !*room.Ahead || hub.SHA != x.liveSHA || room.SHA != newer {
		t.Fatalf("ahead: %+v", b)
	}

	// The hub moves past the room: the room's tip is in the hub's history, so the room is not ahead.
	x.pushToStore(t, newer, "fix/live")
	child := x.commit(t, "fix/live", "again.txt")
	x.pushToStore(t, child, "fix/live")
	run(t, x.clone, "reset", "-q", "--hard", newer)
	b = branchOf(x.ask(t, "repo=o/r&branch=fix/live"), "fix/live")
	hub, room = sourceOf(b, "hub"), sourceOf(b, "room")
	if hub == nil || room == nil || room.Ahead == nil || *room.Ahead || hub.SHA != child || room.SHA != newer {
		t.Fatalf("behind: %+v", b)
	}
}

func TestAMissAnswersNotFoundWithTheClosestAndAsksNoRoom(t *testing.T) {
	x := newURLRig(t)
	before := x.advs.Load()

	a := x.ask(t, "repo=o/rr")
	if a.State != gitsync.URLNotFound || a.Closest == nil || len(a.Closest.Repos) == 0 || a.Closest.Repos[0] != "github/o/r" {
		t.Fatalf("unknown repo: %+v", a)
	}
	if a := x.ask(t, "repo=zzzzzzzz"); a.State != gitsync.URLNotFound || (a.Closest != nil && len(a.Closest.Repos) != 0) {
		t.Fatalf("nothing close: %+v", a)
	}
	// A name the hub has no list entry for is never put to a room, so a lookup is not a way to probe one.
	if n := x.advs.Load(); n != before {
		t.Fatalf("a repository the hub does not know reached the room (%d asks)", n-before)
	}

	a = x.ask(t, "repo=o/r&branch=claude/w")
	if a.State != gitsync.URLNotFound || a.Repo != "github/o/r" || a.Closest == nil || a.Closest.Branches[0] != "claude/w1" {
		t.Fatalf("unknown branch: %+v", a)
	}
	if len(a.Closest.Branches) > gitsync.LookupClosestMax {
		t.Fatalf("closest is not bounded: %v", a.Closest.Branches)
	}
}

func TestAnOfflineRoomAnswersOfflineBeforeAnyoneFetches(t *testing.T) {
	x := newURLRig(t)
	x.pushToStore(t, x.claudeW1, "feat/done")
	x.cancel()
	waitFor(t, 5*time.Second, func() bool { return !x.hub.Has("sg3") })
	posts := x.posts.Load()

	// Asked of that room.
	a := x.ask(t, "repo=o/r&branch=claude/w1&room=SG3")
	if a.State != gitsync.URLOffline || len(a.Offline) != 1 || !strings.Contains(a.Note, "not connected") {
		t.Fatalf("room asked: %+v", a)
	}
	// Not asked of any room: the branch is not on the hub, and the room that synced the repository is gone.
	a = x.ask(t, "repo=o/r&branch=claude/w1")
	if a.State != gitsync.URLOffline || len(a.Offline) != 1 || !strings.EqualFold(a.Offline[0], "SG3") {
		t.Fatalf("no room named: %+v", a)
	}
	// A branch the hub has is still answered, and the room is named as away.
	a = x.ask(t, "repo=o/r&branch=feat/done")
	if a.State != gitsync.URLFound || sourceOf(&a.Branches[0], "hub") == nil || sourceOf(&a.Branches[0], "room") != nil {
		t.Fatalf("hub branch: %+v", a)
	}
	if x.posts.Load() != posts {
		t.Fatal("a fetch was made")
	}
}

func TestABranchTheRoomWouldNotServeIsNeverListed(t *testing.T) {
	x := newURLRig(t)
	// The clone's own main and master are live-card branches here and are not served, and neither are the stash,
	// the notes and a branch with no live card.
	run(t, x.clone, "branch", "main", "hub-main")
	run(t, x.clone, "branch", "master", "hub-main")
	x.setLive("fix/live", "main", "master", "secret/x-not-live")
	x.pushToStore(t, x.liveSHA, "feat/done")

	a := x.ask(t, "repo=o/r")
	var rooms []string
	for _, b := range a.Branches {
		if r := sourceOf(&b, "room"); r != nil {
			rooms = append(rooms, b.Name)
		}
	}
	if strings.Join(rooms, ",") != "claude/w1,fix/live" {
		t.Fatalf("the room's branches are %v, want claude/w1 and fix/live only", rooms)
	}
	raw, _ := json.Marshal(a)
	for _, banned := range []string{"stash", "notes", "secret", "hub-main", "claude/main", x.stash, x.notes, x.secret} {
		if strings.Contains(string(raw), banned) {
			t.Errorf("the answer names %q: %s", banned, raw)
		}
	}
	// main is the HUB's, and no room source for it.
	if m := branchOf(a, "main"); m == nil || sourceOf(m, "room") != nil || sourceOf(m, "hub") == nil {
		t.Fatalf("main: %+v", m)
	}
	for _, name := range []string{"secret/x", "master", "hub-main", "claude/main"} {
		if got := x.ask(t, "repo=o/r&branch="+name); got.State != gitsync.URLNotFound {
			t.Errorf("%s: %+v", name, got)
		}
	}
}

func TestTheURLIsOnTheHostTheCallerReachedTheHubBy(t *testing.T) {
	x := newURLRig(t)
	x.pushToStore(t, x.claudeW1, "claude/w1")
	for _, c := range []struct {
		h      http.Handler
		host   string
		remote string
		base   string
	}{
		{x.proxy, "127.0.0.1:7778", "127.0.0.1:5555", "http://127.0.0.1:7778"},
		{edge.MarkReach(x.proxy, edge.ReachOverlay), "atrium.ziti", offLoopback, "http://atrium.ziti"},
		{edge.MarkReach(x.proxy, edge.ReachZrokPrivate), "abc123.share.zrok.io", offLoopback, "http://abc123.share.zrok.io"},
	} {
		code, body := x.get(t, c.h, c.host, c.remote, "repo=o/r&branch=claude/w1")
		if code != 200 {
			t.Fatalf("%s: %d %s", c.host, code, body)
		}
		var a gitsync.URLAnswer
		_ = json.Unmarshal([]byte(body), &a)
		for _, s := range a.Branches[0].Sources {
			if !strings.HasPrefix(s.URL, c.base+"/git/") {
				t.Errorf("%s: %s source %q is not on %s", c.host, s.Source, s.URL, c.base)
			}
		}
		if len(a.Branches[0].Sources) != 2 {
			t.Errorf("%s: %+v", c.host, a.Branches[0].Sources)
		}
		if strings.Contains(body, "collected_at") {
			t.Errorf("a copy's time is in the answer: %s", body)
		}
	}
	// A Host that is not a name is not put in a URL.
	for _, host := range []string{"evil.example/x@y", "a b", "x.y:80/z"} {
		req := httptest.NewRequest("GET", "http://127.0.0.1:7778/_hub/git/url?repo=o/r", nil)
		req.Host, req.RemoteAddr = host, "127.0.0.1:5555"
		rec := httptest.NewRecorder()
		x.proxy.ServeHTTP(rec, req)
		if rec.Code == http.StatusOK && strings.Contains(rec.Body.String(), host) {
			t.Errorf("Host %q went into a URL: %s", host, rec.Body)
		}
	}
}

func TestOnlyTheReachesOfAFetchMayAskForAURL(t *testing.T) {
	x := newURLRig(t)
	q := "repo=o/r"
	for name, c := range map[string]struct {
		h      http.Handler
		host   string
		remote string
		want   int
	}{
		"loopback":             {x.proxy, "127.0.0.1:7778", "127.0.0.1:5555", 200},
		"overlay":              {edge.MarkReach(x.proxy, edge.ReachOverlay), "atrium.ziti", offLoopback, 200},
		"zrok private":         {edge.MarkReach(x.proxy, edge.ReachZrokPrivate), "atrium.ziti", offLoopback, 200},
		"zrok public":          {edge.MarkReach(x.proxy, edge.ReachZrokPublic), "atrium.ziti", offLoopback, 404},
		"zrok public, as lo":   {edge.MarkReach(x.proxy, edge.ReachZrokPublic), "127.0.0.1:7778", "127.0.0.1:5555", 404},
		"off-machine, no mark": {x.proxy, "atrium.ziti", offLoopback, 403},
	} {
		if code, body := x.get(t, c.h, c.host, c.remote, q); code != c.want {
			t.Errorf("%s = %d %s, want %d", name, code, body, c.want)
		}
	}
	// A public share's answer is the one a path that is not there gets, and says nothing of git.
	_, pub := x.get(t, edge.MarkReach(x.proxy, edge.ReachZrokPublic), "atrium.ziti", offLoopback, q)
	_, none := x.get(t, edge.MarkReach(x.proxy, edge.ReachZrokPublic), "atrium.ziti", offLoopback, "repo=o/r&x=1")
	if pub != none || strings.Contains(strings.ToLower(pub), "git") {
		t.Errorf("a public share was told %q", pub)
	}
	// No repository, and not a GET.
	if code, _ := x.get(t, x.proxy, "127.0.0.1:7778", "127.0.0.1:5555", ""); code != http.StatusBadRequest {
		t.Errorf("no repo = %d", code)
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:7778/_hub/git/url?repo=o/r", nil)
	req.RemoteAddr = "127.0.0.1:5555"
	rec := httptest.NewRecorder()
	x.proxy.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST = %d", rec.Code)
	}
}

func TestARoomsAnswerIsHeldBrieflyAndNotForever(t *testing.T) {
	x := newURLRig(t)
	x.g.LookupTTL = time.Hour
	before := x.advs.Load()
	x.ask(t, "repo=o/r")
	x.ask(t, "repo=o/r&branch=claude/w1")
	x.ask(t, "repo=github/o/r")
	if n := x.advs.Load() - before; n != 1 {
		t.Fatalf("three lookups asked the room %d times, want 1", n)
	}
	x.g.LookupTTL = time.Nanosecond
	x.ask(t, "repo=o/r")
	if n := x.advs.Load() - before; n != 2 {
		t.Fatalf("an expired answer was not asked again (%d)", n)
	}
}

// The tool is the same code over the same board.
func TestTheToolReturnsTheEndpointsJSONAndALine(t *testing.T) {
	x := newURLRig(t)
	x.setLive("fix/live")
	x.pushToStore(t, x.liveSHA, "fix/live")
	c := &controlMCP{board: x.srv.URL, client: x.srv.Client(), audit: func(string, string, string) {}}
	ts := httptest.NewServer(c.handler())
	defer ts.Close()
	cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "v0"}, nil)
	s, err := cl.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL, HTTPClient: &http.Client{Transport: headerTransport{"", ""}},
		DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	call := func(args map[string]any) map[string]any {
		res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_url", Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%v %+v", err, res)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		return m
	}
	same := func(args map[string]any, query string) {
		t.Helper()
		got := call(args)
		resp, err := http.Get(x.srv.URL + "/_hub/git/url?" + query)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var want map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&want); err != nil {
			t.Fatal(err)
		}
		text, _ := got["text"].(string)
		delete(got, "text")
		g, _ := json.Marshal(got)
		w, _ := json.Marshal(want)
		if string(g) != string(w) {
			t.Fatalf("the tool answered\n%s\nthe endpoint\n%s", g, w)
		}
		if text == "" {
			t.Fatalf("no line to read: %v", got)
		}
	}
	same(map[string]any{"repo": "o/r", "branch": "fix/live"}, "repo=o%2Fr&branch=fix%2Flive")
	same(map[string]any{"repo": "o/r"}, "repo=o%2Fr")
	same(map[string]any{"repo": "nope/nope"}, "repo=nope%2Fnope")
	same(map[string]any{"repo": "o/r", "branch": "claude/w1", "room": "SG3"}, "repo=o%2Fr&branch=claude%2Fw1&room=SG3")

	a := call(map[string]any{"repo": "o/r", "branch": "fix/live"})
	text, _ := a["text"].(string)
	if !strings.HasPrefix(text, "fetch it with: git fetch http://") || !strings.Contains(text, " fix/live") {
		t.Fatalf("text %q", text)
	}
	if m := call(map[string]any{"repo": "zzz"}); !strings.HasPrefix(m["text"].(string), "not found") {
		t.Fatalf("miss text %v", m["text"])
	}
	if res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_url",
		Arguments: map[string]any{"repo": " "}}); err == nil && !res.IsError {
		t.Error("an empty repo was taken")
	}
}
