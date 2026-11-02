//go:build integration && git

package link

import (
	"context"
	"encoding/json"
	forgepkg "github.com/dovholuknf/atrium/internal/link/forge"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

type mergeFix struct {
	*pushFix
	st  *hubstore.Store
	url string
}

func newMergeFix(t *testing.T) *mergeFix {
	t.Helper()
	x := &mergeFix{pushFix: newPushFix(t)}
	x.repo(t, boardRepo)
	st, err := hubstore.Open(filepath.Join(x.tmp, "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	x.st = st
	x.p.SetBacklog(st)
	srv := httptest.NewServer(x.p)
	t.Cleanup(srv.Close)
	x.url = srv.URL + "/git/hub/" + boardRepo + ".git"
	x.push(t, "main:refs/heads/claude/main")
	x.p.forgeService().Merges.CardID = func(_ context.Context, _, agent string) string { return agent }
	return x
}

// own makes card the owner of claude/<name> on sg4, as its first push since a release would.
func (x *mergeFix) own(t *testing.T, name, card string) {
	t.Helper()
	ref := "refs/heads/claude/" + name
	if err := x.log.Append(context.Background(), gitsync.PushRow{Repo: boardRepo, Ref: ref, Release: true},
		gitsync.PushRow{Repo: boardRepo, Ref: ref, New: x.refOn(t, ref), Room: "sg4", Card: card}); err != nil {
		t.Fatal(err)
	}
}

func (x *mergeFix) push(t *testing.T, spec string) {
	t.Helper()
	if out, err := gitAs(t, x.work, nil, "push", "-q", "-f", x.url, spec); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

// branch commits one file on a new branch off main and pushes it as claude/<name>.
func (x *mergeFix) branch(t *testing.T, name, file, body string) string {
	t.Helper()
	gitRun(t, x.work, "checkout", "-q", "-B", name, "main")
	sha := x.commit(t, file, body)
	x.push(t, name+":refs/heads/claude/"+name)
	gitRun(t, x.work, "checkout", "-q", "main")
	x.own(t, name, "w1")
	return sha
}

func (x *mergeFix) ask(t *testing.T, branch, by string) (int, hubstore.MergeRequest) {
	t.Helper()
	req := httptest.NewRequest("POST", "http://127.0.0.1:7778/_hub/merge",
		strings.NewReader(js(map[string]any{"repo": boardRepo, "branch": branch, "by": by})))
	req.RemoteAddr = loopAddr
	rec := httptest.NewRecorder()
	x.p.ServeHTTP(rec, req)
	var out struct{ Request hubstore.MergeRequest }
	if rec.Code < 300 {
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
	}
	return rec.Code, out.Request
}

func (x *mergeFix) send(method, target, body string) (int, string) {
	req := httptest.NewRequest(method, "http://127.0.0.1:7778"+target, strings.NewReader(body))
	req.RemoteAddr = loopAddr
	rec := httptest.NewRecorder()
	x.p.ServeHTTP(rec, req)
	return rec.Code, rec.Body.String()
}

// testing puts request n where the merge room would have it: merged in the store and handed to sg4.
func (x *mergeFix) testing(t *testing.T, n int64) hubstore.MergeRequest {
	t.Helper()
	m, err := x.st.MergeMove(hubstore.MergeStep{N: n, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: "sg4"})
	if err != nil {
		t.Fatal(err)
	}
	res, err := x.g.Store().MergeCandidate(context.Background(), m.Repo, m.Branch, m.SHA, n, "Merge")
	if err != nil || res.Candidate == "" {
		t.Fatalf("candidate: %+v %v", res, err)
	}
	m, err = x.st.MergeMove(hubstore.MergeStep{N: n, To: hubstore.MergeTesting, Base: res.Base, Candidate: res.Candidate,
		Token: strings.Repeat("ab", 16)})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

func TestMergeQueueAsksCancelsAndRefuses(t *testing.T) {
	x := newMergeFix(t)
	if code, _ := x.ask(t, "claude/nope", "w1@sg4"); code != http.StatusBadRequest {
		t.Fatalf("an unpushed branch: %d", code)
	}
	x.branch(t, "foo", "foo.txt", "foo")
	if code, _ := x.ask(t, "claude/foo", "@sg4"); code != http.StatusBadRequest {
		t.Fatalf("no card: %d", code)
	}
	if code, _ := x.ask(t, "claude/foo", "w2@sg4"); code != http.StatusForbidden {
		t.Fatalf("another card asked for w1's branch: %d", code)
	}
	if code, _ := x.ask(t, "claude/foo", "w1@other"); code != http.StatusForbidden {
		t.Fatalf("the same name on another room asked for it: %d", code)
	}
	code, m := x.ask(t, "claude/foo", "w1@SG4")
	if code != http.StatusCreated || m.State != hubstore.MergeQueued || m.Requester != "w1" || m.RequesterRoom != "sg4" {
		t.Fatalf("ask: %d %+v", code, m)
	}
	if code, again := x.ask(t, "claude/foo", "w1@sg4"); code != http.StatusOK || again.N != m.N {
		t.Fatalf("asked again: %d %+v", code, again)
	}
	x.p.forgeService().MergeSchedule(context.Background())
	code, body := x.send("GET", "/_hub/merge?open=1", "")
	if code != 200 || !strings.Contains(body, "no merge room") || !strings.Contains(body, `"queued"`) {
		t.Fatalf("held: %d %s", code, body)
	}
	cancel := "/_hub/merge/" + strconv.FormatInt(m.N, 10) + "/cancel"
	if code, _ := x.send("POST", cancel, js(map[string]any{"by": "w2@sg4"})); code != http.StatusForbidden {
		t.Fatalf("another card cancelled it: %d", code)
	}
	if code, body := x.send("POST", cancel, js(map[string]any{"by": "w1@sg4"})); code != 200 || !strings.Contains(body, `"cancelled"`) {
		t.Fatalf("cancel: %d %s", code, body)
	}
	code, body = x.send("POST", "/_hub/merge", js(map[string]any{"repo": boardRepo, "branch": "claude/foo", "by": "w1@sg4"}))
	if code != http.StatusCreated {
		t.Fatalf("asked after a cancel: %d %s", code, body)
	}
	req := httptest.NewRequest("POST", "http://127.0.0.1:7778/_hub/merge", strings.NewReader("{}"))
	req.RemoteAddr = "203.0.113.9:4000"
	rec := httptest.NewRecorder()
	x.p.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("from afar: %d", rec.Code)
	}
}

func TestMergeQueueLandsAGreenRefusesAWrongRoomAndKeepsARed(t *testing.T) {
	x := newMergeFix(t)
	x.branch(t, "foo", "foo.txt", "foo")
	x.commit(t, "main.txt", "main moves on")
	x.push(t, "main:refs/heads/claude/main")
	_, m := x.ask(t, "claude/foo", "w1@sg4")
	m = x.testing(t, m.N)
	report := func(room, verdict string, extra map[string]any) (int, string) {
		body := map[string]any{"n": m.N, "repo": m.Repo, "candidate": m.Candidate, "verdict": verdict, "room": room, "token": m.Token}
		for k, v := range extra {
			body[k] = v
		}
		return x.send("POST", "/_hub/merge/report", js(body))
	}
	if code, _ := report("other", "green", nil); code != http.StatusForbidden {
		t.Fatalf("another room's verdict: %d", code)
	}
	if code, _ := report("sg4", "green", map[string]any{"token": ""}); code != http.StatusForbidden {
		t.Fatalf("a verdict with no token: %d", code)
	}
	if code, _ := report("sg4", "green", map[string]any{"by": "w1@sg4"}); code != http.StatusForbidden {
		t.Fatalf("a card's verdict: %d", code)
	}
	code, body := report("sg4", "green", map[string]any{"output": "ok\n"})
	if code != 200 || !strings.Contains(body, `"landed"`) {
		t.Fatalf("green: %d %s", code, body)
	}
	if got := x.refOn(t, "refs/heads/claude/main"); got != m.Candidate {
		t.Fatalf("claude/main is %s, not %s", got, m.Candidate)
	}
	if got := x.refOn(t, "refs/heads/claude/foo"); got != "" {
		t.Fatalf("claude/foo is still there at %s", got)
	}
	if got := x.refOn(t, "refs/atrium/merge/"+strconv.FormatInt(m.N, 10)); got != "" {
		t.Fatalf("the candidate ref is still there")
	}
	if code, body := report("sg4", "green", nil); code != 200 || !strings.Contains(body, `"landed"`) {
		t.Fatalf("the same verdict again: %d %s", code, body)
	}
	if code, _ := report("sg4", "green", map[string]any{"candidate": strings.Repeat("c", 40)}); code != http.StatusConflict {
		t.Fatalf("a verdict for another candidate: %d", code)
	}

	x.branch(t, "bar", "bar.txt", "bar")
	_, m = x.ask(t, "claude/bar", "w1@sg4")
	m = x.testing(t, m.N)
	code, body = report("sg4", "red", map[string]any{"failing": []string{"\x1b[31mTestA\x1b[0m\r"}, "output": "--- FAIL: TestA\n"})
	if code != 200 || !strings.Contains(body, `"red"`) || !strings.Contains(body, "TestA") {
		t.Fatalf("red: %d %s", code, body)
	}
	if got := x.refOn(t, "refs/heads/claude/bar"); got == "" {
		t.Fatal("a red branch was deleted")
	}
	if got := forgepkg.MergeLine(must(x.st.MergeGet(m.N))); !strings.Contains(got, "failed TestA") {
		t.Fatalf("told: %s", got)
	}
}

func TestMergeQueueConflictAndAMovedMainAreNotLanded(t *testing.T) {
	x := newMergeFix(t)
	x.branch(t, "baz", "main.txt", "theirs")
	x.commit(t, "main.txt", "ours")
	x.push(t, "main:refs/heads/claude/main")
	_, m := x.ask(t, "claude/baz", "w1@sg4")
	x.p.forgeService().Merges.Run.Lock()
	x.p.forgeService().MergeStart(context.Background(), x.st, x.g.Store(), m, "sg4")
	x.p.forgeService().MergeUnlock()
	got := must(x.st.MergeGet(m.N))
	if got.State != hubstore.MergeRefused || !strings.Contains(got.Reason, "main.txt") || !strings.Contains(got.Reason, "no merger could start") {
		t.Fatalf("conflict: %+v", got)
	}

	x.branch(t, "qux", "qux.txt", "qux")
	_, m = x.ask(t, "claude/qux", "w1@sg4")
	m = x.testing(t, m.N)
	x.commit(t, "late.txt", "an operator push")
	x.push(t, "main:refs/heads/claude/main")
	body := js(map[string]any{"n": m.N, "repo": m.Repo, "candidate": m.Candidate, "verdict": "green", "room": "sg4", "token": m.Token})
	if code, b := x.send("POST", "/_hub/merge/report", body); code != 200 {
		t.Fatalf("moved main: %d %s", code, b)
	}
	if got := must(x.st.MergeGet(m.N)); got.State != hubstore.MergeQueued || got.Retries != 1 {
		t.Fatalf("queued again: %+v", got)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

func TestMergeToolAsksForTheWorktreesBranch(t *testing.T) {
	x := newMergeFix(t)
	sha := x.branch(t, "door", "door.txt", "door")
	d := MergeDoor{
		Call: func(ctx context.Context, method, path string, body, out any) error {
			return (&controlMCP{board: x.boardURL(t), client: http.DefaultClient}).Ask(ctx, method, path, "", body, out)
		},
		By: func(*mcp.CallToolRequest) string { return "w1@sg4" },
		Here: func(context.Context) (string, string, string, error) {
			return boardRepo, "claude/door", sha, nil
		},
	}
	_, out, err := d.Handle(context.Background(), nil, mergeInput{Action: "request"})
	if err != nil || out.Request == nil || out.Request.Branch != "claude/door" || out.Request.Requester != "w1" {
		t.Fatalf("request: %+v %v", out, err)
	}
	if _, out, err = d.Handle(context.Background(), nil, mergeInput{Action: "list"}); err != nil || len(out.Requests) != 1 {
		t.Fatalf("list: %+v %v", out, err)
	}
	if _, out, err = d.Handle(context.Background(), nil, mergeInput{Action: "cancel", N: out.Requests[0].N}); err != nil ||
		out.Request.State != hubstore.MergeCancelled {
		t.Fatalf("cancel: %+v %v", out, err)
	}
	if _, _, err = d.Handle(context.Background(), nil, mergeInput{Action: "land"}); err == nil {
		t.Fatal("land is not an action of the tool")
	}
}

func (x *mergeFix) boardURL(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(x.p)
	t.Cleanup(srv.Close)
	return srv.URL
}

func TestMergeMergerAsksWithOfAndLandsTheConflict(t *testing.T) {
	x := newMergeFix(t)
	x.branch(t, "baz", "baz.txt", "baz")
	_, m := x.ask(t, "claude/baz", "w1@sg4")
	must(x.st.MergeMove(hubstore.MergeStep{N: m.N, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: "sg4"}))
	must(x.st.MergeMove(hubstore.MergeStep{N: m.N, To: hubstore.MergeConflict, Reason: "conflicts in baz.txt"}))
	must(x.st.MergeSetMerger(m.N, "m1@sg4"))

	brief := forgepkg.MergerBrief(m, gitsync.MergeResult{Conflicts: []string{"baz.txt"}})
	nb := MergerBranch(m.N)
	if !strings.Contains(brief, "- baz.txt") || !strings.Contains(brief, "branch "+nb+" and of "+strconv.FormatInt(m.N, 10)) {
		t.Fatalf("brief:\n%s", brief)
	}

	sha := x.branch(t, "merge-"+strconv.FormatInt(m.N, 10), "baz.txt", "merged")
	if code, b := x.send("POST", "/_hub/merge", js(map[string]any{"repo": boardRepo, "branch": nb, "by": "w1@sg4", "of": m.N})); code != 403 {
		t.Fatalf("of from a card that is not the merger: %d %s", code, b)
	}
	x.own(t, "merge-"+strconv.FormatInt(m.N, 10), "m1")
	if code, b := x.send("POST", "/_hub/merge", js(map[string]any{"repo": boardRepo, "branch": "claude/baz", "by": "m1@sg4", "of": m.N})); code != 400 {
		t.Fatalf("of from the wrong branch: %d %s", code, b)
	}
	code, b := x.send("POST", "/_hub/merge", js(map[string]any{"repo": boardRepo, "branch": nb, "by": "m1@sg4", "of": m.N}))
	var out struct{ Request hubstore.MergeRequest }
	if code != 201 || json.Unmarshal([]byte(b), &out) != nil || out.Request.Continues != m.N {
		t.Fatalf("of: %d %s", code, b)
	}
	c := out.Request
	must(x.st.MergeMove(hubstore.MergeStep{N: c.N, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: "sg4"}))
	x.p.forgeService().MergeEnd(context.Background(), x.st, hubstore.MergeStep{N: c.N, From: hubstore.MergeMerging, To: hubstore.MergeLanded, Landed: sha})
	if got := must(x.st.MergeGet(m.N)); got.State != hubstore.MergeLanded || got.Landed != sha {
		t.Fatalf("the conflict lands through its merger: %+v", got)
	}
}

func TestMergeAConflictWhoseMergerStoppedIsRefused(t *testing.T) {
	x := newMergeFix(t)
	states := map[string]string{"m1": "running", "m2": "done", "m3": "", "m5": "dead"}
	x.p.forgeService().Merges.CardState = func(_ context.Context, room, agent string) (string, bool) {
		st, ok := states[agent]
		return st, ok
	}
	conflict := func(name, merger string) hubstore.MergeRequest {
		t.Helper()
		x.branch(t, name, name+".txt", name)
		_, m := x.ask(t, "claude/"+name, "w1@sg4")
		must(x.st.MergeMove(hubstore.MergeStep{N: m.N, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: "sg4"}))
		must(x.st.MergeMove(hubstore.MergeStep{N: m.N, To: hubstore.MergeConflict, Reason: "conflicts in " + name + ".txt"}))
		return must(x.st.MergeSetMerger(m.N, merger+"@sg4"))
	}
	working, ended, gone, unread, exited, carried := conflict("wa", "m1"), conflict("wb", "m2"), conflict("wc", "m3"),
		conflict("wd", "m4"), conflict("we", "m5"), conflict("wf", "m2")
	nb := "merge-" + strconv.FormatInt(carried.N, 10)
	x.branch(t, nb, "wf.txt", "merged")
	x.own(t, nb, "m2")
	if code, b := x.send("POST", "/_hub/merge", js(map[string]any{"repo": boardRepo, "branch": "claude/" + nb, "by": "m2@sg4", "of": carried.N})); code != 201 {
		t.Fatalf("of: %d %s", code, b)
	}
	x.p.forgeService().MergeWatchMergers(context.Background())
	for _, c := range []struct {
		m    hubstore.MergeRequest
		want string
	}{{working, ""}, {ended, "ended without asking"}, {gone, "is gone"}, {unread, ""}, {exited, "exited"}, {carried, ""}} {
		got := must(x.st.MergeGet(c.m.N))
		if c.want == "" && got.State != hubstore.MergeConflict || c.want != "" && (got.State != hubstore.MergeRefused || !strings.Contains(got.Reason, c.want)) {
			t.Errorf("%s: %s %s", c.m.Branch, got.State, got.Reason)
		}
	}
}

func TestMergeLandPutsOneCommitOnMainAndLeavesClaudeMain(t *testing.T) {
	x := newMergeFix(t)
	x.push(t, "main:refs/heads/main")
	x.p.forgeService().Merges.Author = func(context.Context) (gitsync.LandAuthor, error) {
		return gitsync.LandAuthor{Name: "o", Email: "o@o"}, nil
	}
	green := func(name string) hubstore.MergeRequest {
		t.Helper()
		x.branch(t, name, name+".txt", name)
		_, m := x.ask(t, "claude/"+name, "w1@sg4")
		m = x.testing(t, m.N)
		body := js(map[string]any{"n": m.N, "repo": m.Repo, "candidate": m.Candidate, "verdict": "green", "room": "sg4", "token": m.Token})
		if code, b := x.send("POST", "/_hub/merge/report", body); code != 200 {
			t.Fatalf("green %s: %d %s", name, code, b)
		}
		return m
	}
	if code, b := x.send("POST", "/_hub/merge/land", js(map[string]any{"repo": boardRepo})); code != 400 || !strings.Contains(b, "nothing has landed") {
		t.Fatalf("an empty land: %d %s", code, b)
	}
	x.branch(t, "in", "in.txt", "in")
	_, in := x.ask(t, "claude/in", "w1@sg4")
	must(x.st.MergeMove(hubstore.MergeStep{N: in.N, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: "sg4"}))
	base := x.refOn(t, "refs/heads/claude/main")
	must(x.st.MergeMove(hubstore.MergeStep{N: in.N, From: hubstore.MergeMerging, To: hubstore.MergeLanded, Base: base, Landed: base}))
	if code, b := x.send("POST", "/_hub/merge/land", js(map[string]any{"repo": boardRepo})); code != 400 || !strings.Contains(b, "nothing has landed") {
		t.Fatalf("an already-in request is a land: %d %s", code, b)
	}
	a, b := green("la"), green("lb")
	claude := x.refOn(t, "refs/heads/claude/main")
	main := x.refOn(t, "refs/heads/main")
	if code, body := x.send("GET", "/_hub/merge", ""); code != 200 || !strings.Contains(body, `"unlanded":{"`+boardRepo+`":2}`) ||
		strings.Contains(body, "pending") || strings.Contains(body, "push") {
		t.Fatalf("the list: %d %s", code, body)
	}
	if code, body := x.send("POST", "/_hub/merge/land", js(map[string]any{"repo": boardRepo, "by": "w1@sg4"})); code != 403 {
		t.Fatalf("a card lands: %d %s", code, body)
	}
	code, body := x.send("POST", "/_hub/merge/land", js(map[string]any{"repo": boardRepo}))
	var l forgepkg.MergeLandOut
	if code != 201 || json.Unmarshal([]byte(body), &l) != nil || len(l.Requests) != 2 || !l.OnMain || l.MainBefore != main ||
		l.ClaudeBefore != claude || strings.Contains(body, "push") {
		t.Fatalf("land: %d %s", code, body)
	}
	if x.refOn(t, "refs/heads/main") != l.Commit || x.refOn(t, "refs/heads/claude/main") != claude ||
		x.refOn(t, "refs/atrium/lands/1") != l.Commit || x.refOn(t, "refs/heads/land/1") != "" {
		t.Fatalf("refs after the land: %s", body)
	}
	if got := must(x.st.MergeGet(a.N)); got.LandN != 1 {
		t.Fatalf("a: %+v", got)
	}
	if got := must(x.st.MergeGet(b.N)); got.LandN != 1 {
		t.Fatalf("b: %+v", got)
	}
	if got := must(x.st.MergeGet(in.N)); got.LandN != 0 {
		t.Fatalf("the already-in request was carried: %+v", got)
	}
	if code, body := x.send("GET", "/_hub/merge", ""); code != 200 || !strings.Contains(body, `"unlanded":{}`) {
		t.Fatalf("the list after: %d %s", code, body)
	}
	code, body = x.send("GET", "/_hub/merge/land?repo="+boardRepo, "")
	if code != 200 || !strings.Contains(body, `"on_main":true`) || !strings.Contains(body, `"unlanded":[]`) {
		t.Fatalf("info: %d %s", code, body)
	}
	c := green("lc")
	if got := must(x.st.MergeGet(c.N)); got.State != hubstore.MergeLanded {
		t.Fatalf("a branch after the land: %+v", got)
	}
	code, body = x.send("POST", "/_hub/merge/land", js(map[string]any{"repo": boardRepo}))
	var l2 forgepkg.MergeLandOut
	if code != 201 || json.Unmarshal([]byte(body), &l2) != nil || l2.MainBefore != l.Commit || x.refOn(t, "refs/heads/main") != l2.Commit {
		t.Fatalf("a land onto the last land: %d %s", code, body)
	}
}

func TestMergeQueueNeverLeavesARequestTesting(t *testing.T) {
	x := newMergeFix(t)
	x.branch(t, "big", "big.txt", "big")
	_, m := x.ask(t, "claude/big", "w1@sg4")
	m = x.testing(t, m.N)
	body := js(map[string]any{"n": m.N, "repo": m.Repo, "candidate": m.Candidate, "verdict": "green", "room": "sg4",
		"token": m.Token, "output": strings.Repeat("ok\n", 350<<10)})
	if code, b := x.send("POST", "/_hub/merge/report", body); code != 200 || !strings.Contains(b, `"landed"`) {
		t.Fatalf("a 1 MiB report: %d %.200s", code, b)
	}
	if got := must(x.st.MergeGet(m.N)); len(got.Output) > hubstore.MergeOutputMax {
		t.Fatalf("the output was kept whole: %d bytes", len(got.Output))
	}

	x.branch(t, "quiet", "quiet.txt", "quiet")
	_, m = x.ask(t, "claude/quiet", "w1@sg4")
	m = x.testing(t, m.N)
	x.p.forgeService().MergeWatch(context.Background())
	if got := must(x.st.MergeGet(m.N)); got.State != hubstore.MergeTesting {
		t.Fatalf("a run within its bound was given up: %+v", got)
	}
	x.p.forgeService().Merges.Now = func() time.Time { return time.Now().Add(forgepkg.MergeSilent + time.Minute) }
	x.p.forgeService().MergeWatch(context.Background())
	got := must(x.st.MergeGet(m.N))
	if got.State != hubstore.MergeQueued || got.Retries != 0 || !strings.Contains(got.Reason, "did not answer") {
		t.Fatalf("a room that never reported: %+v", got)
	}

	must(x.st.MergeMove(hubstore.MergeStep{N: m.N, From: hubstore.MergeQueued, To: hubstore.MergeMerging, Room: "sg4"}))
	x.p.forgeService().MergeRecover()
	if got := must(x.st.MergeGet(m.N)); got.State != hubstore.MergeQueued || got.Retries != 0 {
		t.Fatalf("a restart while merging: %+v", got)
	}

	m = x.testing(t, m.N)
	cancel := "/_hub/merge/" + strconv.FormatInt(m.N, 10) + "/cancel"
	if code, _ := x.send("POST", cancel, js(map[string]any{"by": "w1@sg4"})); code == 200 {
		t.Fatal("a card cancelled a testing request")
	}
	if code, b := x.send("POST", cancel, "{}"); code != 200 || !strings.Contains(b, `"cancelled"`) {
		t.Fatalf("the operator cancels a testing request: %d %s", code, b)
	}
	if x.refOn(t, gitsync.MergeRef(m.N)) != "" {
		t.Fatal("the cancelled candidate's ref is still there")
	}
}

func TestMergeQueueHoldsWhenTheRoomCannotRunAndRefusesNobody(t *testing.T) {
	x := newMergeFix(t)
	var ns []int64
	for _, name := range []string{"h1", "h2", "h3"} {
		x.branch(t, name, name+".txt", name)
		_, m := x.ask(t, "claude/"+name, "w1@sg4")
		ns = append(ns, m.N)
	}
	m := x.testing(t, ns[0])
	body := js(map[string]any{"n": m.N, "repo": m.Repo, "candidate": m.Candidate, "verdict": "cannot", "room": "sg4",
		"token": m.Token, "reason": "this room has no clone of github/o/r"})
	if code, b := x.send("POST", "/_hub/merge/report", body); code != 200 {
		t.Fatalf("cannot: %d %s", code, b)
	}
	x.p.forgeService().MergeSchedule(context.Background())
	for _, n := range ns {
		if got := must(x.st.MergeGet(n)); got.State != hubstore.MergeQueued || got.Retries != 0 {
			t.Fatalf("request %d: %+v", n, got)
		}
	}
	if code, b := x.send("GET", "/_hub/merge?open=1", ""); code != 200 || !strings.Contains(b, `"held":{"github/o/r":"the merge room sg4 could not run it`) {
		t.Fatalf("held: %d %s", code, b)
	}
	x.p.forgeService().Merges.Now = func() time.Time { return time.Now().Add(time.Hour) }
	x.p.forgeService().MergeSchedule(context.Background())
	if _, b := x.send("GET", "/_hub/merge?open=1", ""); strings.Contains(b, `"held":{"github/o/r":"the merge room sg4 could not run it`) {
		t.Fatalf("still held after the wait: %s", b)
	}
}
