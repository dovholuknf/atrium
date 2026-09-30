package link

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

const depsRepo = "github/dovholuknf/atrium"

type depsAudit struct {
	mu    sync.Mutex
	lines []string
}

func (a *depsAudit) Record(room, kind, detail string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.lines = append(a.lines, kind+" "+detail)
}

func (a *depsAudit) Recent(int, string, string) ([]AuditEntry, error) { return nil, nil }

func (a *depsAudit) kinds(kind string) []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	var out []string
	for _, l := range a.lines {
		if strings.HasPrefix(l, kind+" ") {
			out = append(out, l)
		}
	}
	return out
}

type depsFix struct {
	x     *relayPair
	st    *hubstore.Store
	work  string
	c1    string
	audit *depsAudit
}

// newDepsFix is a hub with two attached rooms (m1mini and sg4), a checkout on claude/main
// holding r-037's changelog entry and three backlog files, and the gates wired with the
// first commit as the hub's build.
func newDepsFix(t *testing.T) *depsFix {
	t.Helper()
	x := newRelayPair(t)
	t.Cleanup(x.stop)
	f := &depsFix{x: x, work: filepath.Join(t.TempDir(), "work"), audit: &depsAudit{}}
	if err := os.MkdirAll(f.work, 0o755); err != nil {
		t.Fatal(err)
	}
	run(t, f.work, "init", "-q", "-b", "claude/main")
	f.write(t, "docs/backlog/runtime/r-037.md", "# r-037. The first\n\nStatus: BUILT\n")
	f.write(t, "docs/backlog/runtime/r-038.md", "# r-038. The second\n\nStatus: not started\n")
	f.write(t, "docs/backlog/runtime/r-039.md", "# r-039: The third\n\nStatus: design\n")
	f.c1 = f.commit(t, "changelog/runtime/2026-09-30-r-037.md", "r-037 landed")
	x.proxy.SetGit(&gitsync.Hub{Dir: t.TempDir(), Rooms: x.hub.GitRooms(), Runner: gitsync.NewRunner(),
		Repos: func() ([]gitsync.Repo, error) {
			return []gitsync.Repo{{Name: depsRepo, Checkout: f.work, Branch: "claude/main"}}, nil
		}})
	st, err := hubstore.Open(filepath.Join(t.TempDir(), "hub.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	f.st = st
	x.proxy.SetAuditLog(f.audit)
	x.proxy.SetDeps(st, f.c1)
	return f
}

func (f *depsFix) write(t *testing.T, name, text string) {
	t.Helper()
	p := filepath.Join(f.work, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
}

// commit writes one file and commits everything, answering the new commit.
func (f *depsFix) commit(t *testing.T, name, text string) string {
	t.Helper()
	f.write(t, name, text)
	run(t, f.work, "add", "-A")
	run(t, f.work, "-c", "user.name=t", "-c", "user.email=t@t", "-c", "commit.gpgsign=false",
		"commit", "-q", "-m", name)
	return run(t, f.work, "rev-parse", "HEAD")
}

func (f *depsFix) do(t *testing.T, method, path string, body any, hdr map[string]string) (int, map[string]any) {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		rdr = bytes.NewReader(raw)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, _ := http.NewRequest(method, f.x.board+path, rdr)
	req.Header.Set("Content-Type", "application/json")
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	var out map[string]any
	_ = json.NewDecoder(res.Body).Decode(&out)
	return res.StatusCode, out
}

func (f *depsFix) add(t *testing.T, item string, on ...string) []map[string]any {
	t.Helper()
	code, out := f.do(t, http.MethodPost, "/_hub/deps", map[string]any{"item": item, "waits_on": on,
		"why": "test", "added_by": "atrium-87300@sg4"}, nil)
	if code != 200 {
		t.Fatalf("add %s on %v = %d %v", item, on, code, out)
	}
	return gatesOf(out)
}

func (f *depsFix) list(t *testing.T, item string) []map[string]any {
	t.Helper()
	code, out := f.do(t, http.MethodGet, "/_hub/deps?item="+item, nil, nil)
	if code != 200 {
		t.Fatalf("list %s = %d %v", item, code, out)
	}
	return gatesOf(out)
}

func gatesOf(out map[string]any) []map[string]any {
	var gs []map[string]any
	raw, _ := out["gates"].([]any)
	for _, g := range raw {
		gs = append(gs, g.(map[string]any))
	}
	return gs
}

func stateOf(gs []map[string]any, target string) (string, string) {
	for _, g := range gs {
		if g["target"] == target {
			reason, _ := g["reason"].(string)
			return g["state"].(string), reason
		}
	}
	return "", ""
}

func TestAnItemGateIsMetWhenItsChangelogLandsAndStaysMet(t *testing.T) {
	f := newDepsFix(t)
	if s, why := stateOf(f.add(t, "r-038", "r-037"), "r-037"); s != "met" ||
		!strings.Contains(why, "changelog/runtime/2026-09-30-r-037.md on claude/main") {
		t.Fatalf("landed item = %s %q", s, why)
	}
	if s, why := stateOf(f.add(t, "r-039", "r-050"), "r-050"); s != "open" ||
		!strings.Contains(why, "not landed: no changelog/*/*-r-050.md on claude/main") {
		t.Fatalf("absent item = %s %q", s, why)
	}
	f.commit(t, "changelog/runtime/2026-09-30-r-050.md", "r-050")
	if s, _ := stateOf(f.list(t, "r-039"), "r-050"); s != "met" {
		t.Fatalf("after landing = %s", s)
	}
	gs, _ := f.st.Gates(depsRepo, "r-039", false)
	if len(gs) != 1 || gs[0].MetBy != "atrium" {
		t.Fatalf("stored = %+v", gs)
	}
	// Re-signed history that drops the file does not un-land it.
	run(t, f.work, "reset", "-q", "--hard", f.c1)
	if s, _ := stateOf(f.list(t, "r-039"), "r-050"); s != "met" {
		t.Fatalf("met is not sticky: %s", s)
	}
}

func TestAPartialLandingDoesNotMeetTheWholeItem(t *testing.T) {
	f := newDepsFix(t)
	f.commit(t, "changelog/runtime/2026-09-30-r-007-3.md", "stage 3")
	if s, _ := stateOf(f.add(t, "r-038", "r-007"), "r-007"); s != "open" {
		t.Fatalf("r-007-3 met r-007: %s", s)
	}
}

func TestARenamedItemIsSeenUnderEitherName(t *testing.T) {
	f := newDepsFix(t)
	f.add(t, "r-038", "r-new-x")
	if code, out := f.do(t, http.MethodPost, "/_hub/deps/rename",
		map[string]any{"from": "r-new-x", "to": "r-070"}, nil); code != 200 || out["moved"].(float64) != 1 {
		t.Fatalf("rename = %d %v", code, out)
	}
	f.commit(t, "changelog/runtime/2026-09-30-r-070.md", "numbered")
	if s, _ := stateOf(f.list(t, "r-038"), "r-070"); s != "met" {
		t.Fatalf("by the new name = %s", s)
	}
	// A slug that landed before its rename still counts.
	f.commit(t, "changelog/runtime/2026-09-30-r-new-y.md", "slug")
	f.add(t, "r-039", "r-071")
	if code, _ := f.do(t, http.MethodPost, "/_hub/deps/rename", map[string]any{"from": "r-new-y", "to": "r-071"}, nil); code != 200 {
		t.Fatal(code)
	}
	if s, why := stateOf(f.list(t, "r-039"), "r-071"); s != "met" || !strings.Contains(why, "r-new-y.md") {
		t.Fatalf("landed as the slug = %s %q", s, why)
	}
}

func TestShaLiveAndRoomConditions(t *testing.T) {
	f := newDepsFix(t)
	c2 := f.commit(t, "changelog/runtime/2026-09-30-r-050.md", "after the build")
	gs := f.add(t, "r-038", "sha:"+f.c1[:8], "sha:deadbeef", "live:r-037", "live:r-050", "room:sg4", "room:sg3",
		"f-006 migration on m1mini")
	for target, want := range map[string]string{
		"sha:" + f.c1[:8]: "met", "sha:deadbeef": "open", "live:r-037": "met", "live:r-050": "open",
		"room:sg4": "met", "room:sg3": "open", "f-006 migration on m1mini": "open",
	} {
		if s, why := stateOf(gs, target); s != want {
			t.Errorf("%s = %s (%s), want %s", target, s, why, want)
		}
	}
	if _, why := stateOf(gs, "live:r-050"); !strings.Contains(why, "not in the hub build "+f.c1[:7]) {
		t.Errorf("live after the build says %q", why)
	}
	if _, why := stateOf(gs, "f-006 migration on m1mini"); !strings.Contains(why, "cleared by a human only") {
		t.Errorf("free text says %q", why)
	}
	// A hub built from the later commit has r-050 live.
	f.x.proxy.SetDeps(f.st, c2)
	if s, _ := stateOf(f.list(t, "r-038"), "live:r-050"); s != "met" {
		t.Errorf("live in a later build = %s", s)
	}
	// A dev build cannot say.
	f.x.proxy.SetDeps(f.st, "")
	if s, why := stateOf(f.add(t, "r-039", "live:r-037"), "live:r-037"); s != "open" || !strings.Contains(why, "hub build unknown") {
		t.Errorf("dev build = %s %q", s, why)
	}
}

func TestAGitFailureLeavesTheGateOpenWithTheReason(t *testing.T) {
	f := newDepsFix(t)
	f.x.proxy.SetGit(&gitsync.Hub{Dir: t.TempDir(), Rooms: f.x.hub.GitRooms(), Runner: gitsync.NewRunner(),
		Repos: func() ([]gitsync.Repo, error) {
			return []gitsync.Repo{{Name: depsRepo, Checkout: filepath.Join(t.TempDir(), "gone"), Branch: "claude/main"}}, nil
		}})
	if s, why := stateOf(f.add(t, "r-038", "r-037"), "r-037"); s != "open" || !strings.Contains(why, "could not read claude/main") {
		t.Fatalf("a checkout that is not there = %s %q", s, why)
	}
}

func TestALoopIsRefusedOverTheRoute(t *testing.T) {
	f := newDepsFix(t)
	f.add(t, "r-040", "r-041")
	code, out := f.do(t, http.MethodPost, "/_hub/deps", map[string]any{"item": "r-041", "waits_on": []string{"r-040"}}, nil)
	if code != 409 || !strings.Contains(out["error"].(string), "r-041 waits on r-040, which waits on r-041") {
		t.Fatalf("loop = %d %v", code, out)
	}
}

func TestOnlyAHumanWithAReasonClearsAGate(t *testing.T) {
	f := newDepsFix(t)
	id := f.add(t, "r-038", "f-006 migration on m1mini")[0]["id"].(string)
	if code, _ := f.do(t, http.MethodPost, "/_hub/deps/clear", map[string]any{"id": id, "why": "done by hand"},
		map[string]string{AgentHeader: "sa1"}); code != 403 {
		t.Fatalf("an agent cleared a gate: %d", code)
	}
	if code, _ := f.do(t, http.MethodPost, "/_hub/deps/clear", map[string]any{"id": id}, nil); code != 400 {
		t.Fatalf("a clear with no reason: %d", code)
	}
	code, out := f.do(t, http.MethodPost, "/_hub/deps/clear", map[string]any{"id": id, "why": "done by hand"}, nil)
	if code != 200 {
		t.Fatalf("clear = %d %v", code, out)
	}
	g, _ := f.st.Gate(id)
	if g.MetBy != "human" || !strings.Contains(g.MetWhy, "done by hand") {
		t.Fatalf("stored = %+v", g)
	}
	if lines := f.audit.kinds("deps-clear"); len(lines) != 1 || !strings.Contains(lines[0], "done by hand") ||
		!strings.Contains(lines[0], "127.0.0.1") {
		t.Fatalf("audit = %v", lines)
	}
	if code, _ := f.do(t, http.MethodPost, "/_hub/deps/clear", map[string]any{"id": id, "why": "again"}, nil); code != 409 {
		t.Fatalf("a second clear = %d", code)
	}
}

func TestALaunchForAGatedItemIsRefused(t *testing.T) {
	f := newDepsFix(t)
	f.add(t, "r-039", "r-050")
	ctx := context.Background()
	msg, blocked := f.x.proxy.launchGate(ctx, "r-039: the third")
	if !blocked || !strings.Contains(msg, "r-039 waits on: r-050 (not landed") ||
		!strings.Contains(msg, "atrium_deps check r-039") {
		t.Fatalf("gated = %v %q", blocked, msg)
	}
	if _, blocked := f.x.proxy.launchGate(ctx, "r-099: no gate"); blocked {
		t.Fatal("an item with no gate was refused")
	}
	if _, blocked := f.x.proxy.launchGate(ctx, "fix the build"); blocked {
		t.Fatal("a title with no item was refused")
	}
	// Through the tool, before anything reaches a room.
	_, _, err := f.x.proxy.ctl.launchHandler(ctx, nil, launchInput{Cwd: "C:/w", Title: "r-039: the third"})
	var re *refusedError
	if !errors.As(err, &re) || !strings.Contains(re.msg, "r-039 waits on") {
		t.Fatalf("tool = %v", err)
	}
	f.commit(t, "changelog/runtime/2026-09-30-r-050.md", "r-050")
	if _, blocked := f.x.proxy.launchGate(ctx, "r-039: the third"); blocked {
		t.Fatal("still refused with every gate met")
	}
}

func TestTheWaiterIsToldOnceWhenTheLastGateClears(t *testing.T) {
	f := newDepsFix(t)
	gs := f.add(t, "r-039", "r-050", "room:sg3")
	ctx := context.Background()
	f.commit(t, "changelog/runtime/2026-09-30-r-050.md", "r-050")
	f.x.proxy.depsTick(ctx)
	if got := f.x.sg4.messages(); len(got) != 0 {
		t.Fatalf("told while room:sg3 is open: %+v", got)
	}
	var roomGate string
	for _, g := range gs {
		if g["target"] == "room:sg3" {
			roomGate = g["id"].(string)
		}
	}
	if code, _ := f.do(t, http.MethodPost, "/_hub/deps/clear", map[string]any{"id": roomGate, "why": "sg3 is retired"}, nil); code != 200 {
		t.Fatal(code)
	}
	f.x.proxy.depsTick(ctx)
	f.x.proxy.depsTick(ctx)
	got := f.x.sg4.messages()
	if len(got) != 1 || got[0]["id"] != "s1" || got[0]["from"] != depsTellFrom ||
		!strings.HasPrefix(got[0]["text"], "r-039 is ready: r-050 landed") ||
		!strings.Contains(got[0]["text"], "sg3 is retired") {
		t.Fatalf("messages = %+v", got)
	}
}

func TestReadyListsWhatHasNotLandedAndIsNotGated(t *testing.T) {
	f := newDepsFix(t)
	f.add(t, "r-038", "r-050")
	code, out := f.do(t, http.MethodGet, "/_hub/deps/ready?dept=runtime", nil, nil)
	if code != 200 {
		t.Fatalf("ready = %d %v", code, out)
	}
	items, _ := out["items"].([]any)
	if len(items) != 1 {
		t.Fatalf("items = %v, want r-039 only", items)
	}
	it := items[0].(map[string]any)
	if it["id"] != "r-039" || it["title"] != "The third" || it["status"] != "design" {
		t.Fatalf("item = %v", it)
	}
	if code, _ := f.do(t, http.MethodGet, "/_hub/deps/ready?dept=../x", nil, nil); code != 409 {
		t.Fatalf("a bad dept = %d", code)
	}
}

func TestTheDepsToolHasNoClearAndIsNotAWorkers(t *testing.T) {
	if inClass("atrium_deps", classWorker) {
		t.Fatal("a worker is served atrium_deps")
	}
	f := newDepsFix(t)
	_, _, err := f.x.proxy.ctl.depsHandler(context.Background(), nil, depsInput{Action: "clear", Item: "r-038"})
	if err == nil || !strings.Contains(err.Error(), "there is no clear") {
		t.Fatalf("clear = %v", err)
	}
	_, out, err := f.x.proxy.ctl.depsHandler(context.Background(), nil,
		depsInput{Action: "add", Item: "r-038", WaitsOn: []string{"r-050"}})
	if err != nil || len(out.Gates) != 1 || out.Gates[0].State != "open" {
		t.Fatalf("add = %+v %v", out, err)
	}
	_, out, err = f.x.proxy.ctl.depsHandler(context.Background(), nil, depsInput{Action: "check", Item: "r-038"})
	if err != nil || out.Ready == nil || *out.Ready {
		t.Fatalf("check = %+v %v", out, err)
	}
	_, out, err = f.x.proxy.ctl.depsHandler(context.Background(), nil, depsInput{Action: "ready", Dept: "runtime"})
	if err != nil || len(out.Items) != 1 || out.Items[0].ID != "r-039" {
		t.Fatalf("ready = %+v %v", out, err)
	}
}
