package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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
// docs/fabric/hub-forge-design.md section 4.

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
	for _, name := range []string{"secret/x", "hub-main", "claude/main"} {
		if got := x.ask(t, "repo=o/r&branch="+name); got.State != gitsync.URLNotFound {
			t.Errorf("%s: %+v", name, got)
		}
	}
	// master is on the rig's forge, so the hub fetches it from there: the room's live-card master is still not served.
	if got := x.ask(t, "repo=o/r&branch=master"); got.State == gitsync.URLFound {
		if m := branchOf(got, "master"); m == nil || sourceOf(m, "room") != nil {
			t.Errorf("master: %+v", got)
		}
	} else if got.State != gitsync.URLNotFound {
		t.Errorf("master: %+v", got)
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

// A branch pushed by a room that is not attached is on the hub, and the room is named as away: its work in progress
// may be newer than what was pushed, and cannot be read now.
func TestABranchPushedByARoomThatIsGoneNamesTheRoomAsAway(t *testing.T) {
	x := newURLRig(t)
	log := &gitsync.MemPushLog{}
	x.g.PushLog = log
	x.pushToStore(t, x.claudeW1, "feat/done")
	batch, err := log.Begin(context.Background(), gitsync.PushRow{Repo: "github/o/r", Ref: "refs/heads/feat/done",
		New: x.claudeW1, Room: "m1mini", Card: "c1", At: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	if err := log.Settle(context.Background(), batch, "refs/heads/feat/done"); err != nil {
		t.Fatal(err)
	}
	a := x.ask(t, "repo=o/r&branch=feat/done")
	if a.State != gitsync.URLFound || len(a.Offline) != 1 || a.Offline[0] != "m1mini" {
		t.Fatalf("%+v", a)
	}
	// Asked of that room, it is not connected.
	if a := x.ask(t, "repo=o/r&branch=feat/done&room=m1mini"); a.State != gitsync.URLOffline {
		t.Fatalf("%+v", a)
	}
}

// ── a card on another room ─────────────────────────────────────────────────────────────────────────────────

// toolAs is a session of the control tool as a card of a room (or, with no agent, as the operator), and a call of
// atrium_git_url that returns the decoded answer and its line.
func (x *urlRig) toolAs(t *testing.T, agent, room string) func(args map[string]any) (gitsync.URLAnswer, string) {
	t.Helper()
	c := &controlMCP{board: x.srv.URL, client: x.srv.Client(), audit: func(string, string, string) {}}
	ts := httptest.NewServer(c.handler())
	t.Cleanup(ts.Close)
	cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "v0"}, nil)
	s, err := cl.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL, HTTPClient: &http.Client{Transport: headerTransport{agent, room}},
		DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return func(args map[string]any) (gitsync.URLAnswer, string) {
		t.Helper()
		res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_url", Arguments: args})
		if err != nil || res.IsError {
			t.Fatalf("%v %+v", err, res)
		}
		raw, _ := json.Marshal(res.StructuredContent)
		var out struct {
			gitsync.URLAnswer
			Text string `json:"text"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatal(err)
		}
		return out.URLAnswer, out.Text
	}
}

// forwarder stands a card's room's hub forwarder up in front of the hub's board, the way r-hub-remote's tests do, and
// has the room say that is where it is. The forwarder reaches the hub's store as the hub's own loopback.
func (x *urlRig) forwarder(t *testing.T) (base string, cards *gitsync.CardTokens) {
	t.Helper()
	cards = &gitsync.CardTokens{}
	hubURL, _ := url.Parse(x.srv.URL)
	fw := httptest.NewServer(&gitsync.HubForwarder{
		Auth: cards,
		Push: func() string { return "hub" },
		Transport: func() (http.RoundTripper, error) {
			return rtFunc(func(r *http.Request) (*http.Response, error) {
				out := r.Clone(r.Context())
				out.URL.Scheme, out.URL.Host, out.Host = hubURL.Scheme, hubURL.Host, hubURL.Host
				return http.DefaultTransport.RoundTrip(out)
			}), nil
		},
	})
	t.Cleanup(fw.Close)
	base = fw.URL + "/git/"
	x.setRemote(base)
	return base, cards
}

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// A card on another room is handed a URL on ITS room's forwarder, not the hub's loopback, and a real git fetch of it
// with the card's token gets the branch out of the hub's store.
func TestACardOnAnotherRoomIsGivenItsRoomsForwarderAndFetchesThroughIt(t *testing.T) {
	x := newURLRig(t)
	x.setLive("fix/live")
	x.pushToStore(t, x.liveSHA, "fix/live")
	base, cards := x.forwarder(t)
	ans, text := x.toolAs(t, "card1", "SG3")(map[string]any{"repo": "o/r", "branch": "fix/live"})

	hub := sourceOf(branchOf(ans, "fix/live"), "hub")
	if hub == nil || hub.URL != base+"hub/github/o/r.git" {
		t.Fatalf("the hub source is %+v, want a URL on the forwarder %s", hub, base)
	}
	if !strings.HasPrefix(text, "fetch it with: git fetch "+hub.URL+" fix/live") {
		t.Fatalf("the line is %q", text)
	}
	// The operator, asking the same, gets the hub's own address.
	op, _ := x.toolAs(t, "", "")(map[string]any{"repo": "o/r", "branch": "fix/live"})
	if h := sourceOf(branchOf(op, "fix/live"), "hub"); h == nil || !strings.HasPrefix(h.URL, x.srv.URL+"/git/hub/") {
		t.Fatalf("the operator's hub source is %+v, want one on %s", h, x.srv.URL)
	}

	// The URL is fetched through the forwarder, with the card's token and nothing else.
	tok, err := cards.Mint("card1")
	if err != nil {
		t.Fatal(err)
	}
	env := append(gitsync.PushEnv(base, tok, 0), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1")
	dst := t.TempDir()
	gitRun(t, dst, "init", "-q")
	if out, err := gitsync.Default.GitEnv(context.Background(), dst, env, "fetch", "-q", hub.URL, "fix/live"); err != nil {
		t.Fatalf("fetch %s: %v %s", hub.URL, err, out)
	}
	if got := gitRun(t, dst, "rev-parse", "FETCH_HEAD"); got != x.liveSHA {
		t.Fatalf("fetched %s, want %s", got, x.liveSHA)
	}
	// And with no token the same URL is refused: the forwarder is the card's door and not an open one.
	noTok := []string{"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1"}
	if _, err := gitsync.Default.GitEnv(context.Background(), dst, noTok, "fetch", "-q", hub.URL, "fix/live"); err == nil {
		t.Fatal("the forwarder served a fetch with no card token")
	}
}

// A room's work in progress is passed through on the hub's board only, and a card's room has no forwarder for it. So a
// card is given no URL for it, with the way on, and the operator still is.
func TestACardIsGivenNoURLForARoomsWorkInProgress(t *testing.T) {
	x := newURLRig(t)
	x.setLive("fix/live")
	base, _ := x.forwarder(t)
	card := x.toolAs(t, "card1", "SG3")

	ans, text := card(map[string]any{"repo": "o/r", "branch": "claude/w1"})
	room := sourceOf(branchOf(ans, "claude/w1"), "room")
	if room == nil || room.URL != "" || room.Note != gitsync.NoRoomForCards {
		t.Fatalf("a card's room source is %+v, want no URL and the note", room)
	}
	if strings.Contains(text, "git fetch") || !strings.Contains(text, "atrium_git_push") {
		t.Fatalf("the line is %q, want no fetch and the way on", text)
	}
	// A branch that is on both: the hub's URL is the one the line gives, and the room's ahead work is not.
	x.pushToStore(t, x.liveSHA, "fix/live")
	x.commit(t, "fix/live", "more.txt")
	both, line := card(map[string]any{"repo": "o/r", "branch": "fix/live"})
	b := branchOf(both, "fix/live")
	if r := sourceOf(b, "room"); r == nil || r.URL != "" {
		t.Fatalf("the room source of a card is %+v", r)
	}
	if h := sourceOf(b, "hub"); h == nil || h.URL != base+"hub/github/o/r.git" {
		t.Fatalf("the hub source is %+v", h)
	}
	if !strings.Contains(line, "git fetch "+base+"hub/github/o/r.git fix/live") || !strings.Contains(line, "atrium_git_push") {
		t.Fatalf("the line is %q", line)
	}
	// The operator is still given the room's pass-through.
	op, _ := x.toolAs(t, "", "")(map[string]any{"repo": "o/r", "branch": "claude/w1"})
	if r := sourceOf(branchOf(op, "claude/w1"), "room"); r == nil || !strings.Contains(r.URL, "/git/room/") {
		t.Fatalf("the operator's room source is %+v", r)
	}
}

// A room that does not say where its forwarder is (older than it, or not answering), or that says an address that is
// not a forwarder on its own loopback, leaves its card with no URL and the sentence why.
func TestACardWhoseRoomDoesNotSayWhereItsForwarderIsIsGivenNoURL(t *testing.T) {
	x := newURLRig(t)
	x.setLive("fix/live")
	x.pushToStore(t, x.liveSHA, "fix/live")
	for name, remote := range map[string]string{
		"a room that predates it":  "",
		"another host":             "http://evil.example:7777/git/",
		"another path":             "http://127.0.0.1:7777/",
		"a credential in the base": "http://u:p@127.0.0.1:7777/git/",
		"a secure scheme":          "https://127.0.0.1:7777/git/",
	} {
		x.setRemote(remote)
		ans, text := x.toolAs(t, "card1", "SG3")(map[string]any{"repo": "o/r", "branch": "fix/live"})
		hub := sourceOf(branchOf(ans, "fix/live"), "hub")
		if hub == nil || hub.URL != "" || hub.Note != gitsync.NoHubRemote {
			t.Fatalf("%s: the hub source is %+v, want no URL and the note", name, hub)
		}
		if strings.Contains(text, "git fetch") {
			t.Fatalf("%s: the line is %q", name, text)
		}
	}
}

func TestForwarderBaseIsOnlyAForwarderOnTheRoomsOwnLoopback(t *testing.T) {
	for base, ok := range map[string]bool{
		"http://127.0.0.1:7777/git/":         true,
		"http://localhost:7777/git/":         true,
		"http://[::1]:7777/git/":             true,
		"":                                   false,
		"http://127.0.0.1:7777/git":          false,
		"http://127.0.0.1:7777/git/hub/":     false,
		"http://127.0.0.1/git/":              false,
		"http://127.0.0.1:99999/git/":        false,
		"http://10.0.0.5:7777/git/":          false,
		"http://127.0.0.1:7777/git/?x=1":     false,
		"https://127.0.0.1:7777/git/":        false,
		"http://u@127.0.0.1:7777/git/":       false,
		"http://127.0.0.1.evil.io:7777/git/": false,
	} {
		if got := forwarderBase(base); (got != "") != ok {
			t.Errorf("forwarderBase(%q) = %q, want ok=%v", base, got, ok)
		}
	}
}

// The Host check is the second line, behind the hosts guard that refuses a Host the hub does not answer to: called
// directly on the overlay (where a Host is not held to loopback), serveGitURL still refuses a Host that is not a name
// and a port, so a URL is never made of one.
func TestAHostThatIsNotANameAndAPortIsNotMadeIntoAURL(t *testing.T) {
	x := newURLRig(t)
	call := func(host string) int {
		req := httptest.NewRequest("GET", "http://127.0.0.1:7778/_hub/git/url?repo=o%2Fr", nil)
		req.Host, req.RemoteAddr = host, offLoopback
		rec := httptest.NewRecorder()
		edge.MarkReach(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			x.proxy.serveGitURL(w, r, x.g, func(code int, msg string) { w.WriteHeader(code) })
		}), edge.ReachOverlay).ServeHTTP(rec, req)
		return rec.Code
	}
	for _, host := range []string{"hub.example:7778/x", "a b", "u@hub", "hub:1:2", "hub\"", "", "hub:", "-hub"} {
		if code := call(host); code != http.StatusBadRequest {
			t.Errorf("Host %q answered %d, want 400", host, code)
		}
	}
	for _, host := range []string{"127.0.0.1:7778", "[::1]:7778", "hub.local", "hub-1.example.com:443", "atrium.ziti"} {
		if code := call(host); code != http.StatusOK {
			t.Errorf("Host %q answered %d, want 200", host, code)
		}
	}
}
