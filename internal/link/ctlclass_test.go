package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// headerTransport stamps a session's identity on every request, as Claude Code
// does with the headers in its MCP config.
type headerTransport struct{ agent, room string }

func (h headerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if h.agent != "" {
		r.Header.Set(AgentHeader, h.agent)
	}
	if h.room != "" {
		r.Header.Set(RoomHeader, h.room)
	}
	return http.DefaultTransport.RoundTrip(r)
}

// classHarness is the control handler over a fake board that counts task lookups.
type classHarness struct {
	c       *controlMCP
	mcpURL  string
	board   *httptest.Server
	lookups *atomic.Int32
	audits  *[]auditLine
	mu      sync.Mutex
	clock   time.Time
}

func newClassHarness(t *testing.T, tasks []map[string]any) *classHarness {
	t.Helper()
	h := &classHarness{lookups: &atomic.Int32{}, clock: time.Now()}
	h.board = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			h.lookups.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new1", "wire_name": "kid"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(h.board.Close)
	var lines []auditLine
	h.audits = &lines
	h.c = &controlMCP{board: h.board.URL, client: h.board.Client(),
		audit: func(room, kind, detail string) { lines = append(lines, auditLine{room, kind, detail}) },
		now: func() time.Time {
			h.mu.Lock()
			defer h.mu.Unlock()
			return h.clock
		}}
	ts := httptest.NewServer(h.c.handler())
	t.Cleanup(ts.Close)
	h.mcpURL = ts.URL
	return h
}

func (h *classHarness) advance(d time.Duration) {
	h.mu.Lock()
	h.clock = h.clock.Add(d)
	h.mu.Unlock()
}

// connect opens a real MCP session as one caller.
func (h *classHarness) connect(t *testing.T, agent, room string) *mcp.ClientSession {
	t.Helper()
	cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "v0"}, nil)
	sess, err := cl.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint:             h.mcpURL,
		HTTPClient:           &http.Client{Transport: headerTransport{agent, room}},
		DisableStandaloneSSE: true,
		MaxRetries:           -1,
	}, nil)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	return sess
}

func toolNames(t *testing.T, s *mcp.ClientSession) []string {
	t.Helper()
	res, err := s.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatalf("tools/list: %v", err)
	}
	var names []string
	for _, tl := range res.Tools {
		names = append(names, tl.Name)
	}
	sort.Strings(names)
	return names
}

// workerSeven is the six a worker talks and reports with, and atrium_publish, because workers write the reports, and
// atrium_git_push, because the cards that push to the hub are workers, and atrium_git_url, because the cards that read code they do not have are too.
// The name is the count it had before those.
var workerSeven = []string{"atrium_alias", "atrium_blocked", "atrium_done", "atrium_git_push", "atrium_git_url", "atrium_peers", "atrium_publish", "atrium_report", "atrium_resources", "atrium_say", "atrium_status", "atrium_task"}

// fullFifteen is f-021's eleven, f-019's two git tools, atrium_deps and atrium_deploy, and atrium_model, and atrium_publish,
// all full-only. The name is the count it had before atrium_model.
var fullFifteen = []string{"atrium_alias", "atrium_backlog", "atrium_blocked", "atrium_cull", "atrium_deploy", "atrium_deps", "atrium_done", "atrium_exit",
	"atrium_git_collect", "atrium_git_push", "atrium_git_sync", "atrium_git_url", "atrium_launch", "atrium_model", "atrium_peers", "atrium_publish", "atrium_report", "atrium_reports", "atrium_resources",
	"atrium_say", "atrium_status", "atrium_task", "atrium_wake_after_restart", "restart_atrium"}

func sameNames(t *testing.T, who string, got, want []string) {
	t.Helper()
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("%s tools = %v, want %v", who, got, want)
	}
}

var classTasks = []map[string]any{
	{"id": "w1", "wire_name": "worker1", "status": "working", "tags": []string{"atrium:subagent", "origin:agent"}},
	{"id": "d1", "wire_name": "boss", "status": "working", "tags": []string{"atrium:director", "origin:agent"}},
	{"id": "u1", "wire_name": "human", "status": "working"},
	{"id": "b1", "wire_name": "both", "status": "working", "tags": []string{"atrium:subagent", "atrium:director"}},
}

func TestWorkerSeesSixToolsAndCullIsUnknown(t *testing.T) {
	h := newClassHarness(t, classTasks)
	s := h.connect(t, "worker1", "alpha")
	sameNames(t, "worker", toolNames(t, s), workerSeven)

	for _, tool := range []string{"atrium_cull", "atrium_model"} {
		_, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: tool,
			Arguments: map[string]any{"card": "x", "model": "opus"}})
		if err == nil || !strings.Contains(strings.ToLower(err.Error()), "unknown tool") {
			t.Fatalf("%s by a worker: err = %v, want unknown tool", tool, err)
		}
	}
}

func TestDirectorUntaggedAndBothGetEveryTool(t *testing.T) {
	h := newClassHarness(t, classTasks)
	for _, who := range []string{"boss", "human", "both"} {
		sameNames(t, who, toolNames(t, h.connect(t, who, "alpha")), fullFifteen)
	}
}

func TestUnknownAgentAndNoHeaderGetEveryTool(t *testing.T) {
	h := newClassHarness(t, classTasks)
	sameNames(t, "unknown agent", toolNames(t, h.connect(t, "nobody", "alpha")), fullFifteen)
	sameNames(t, "no header", toolNames(t, h.connect(t, "", "")), fullFifteen)
}

func TestFailedLookupFailsOpenToEveryTool(t *testing.T) {
	h := newClassHarness(t, classTasks)
	h.board.Close()
	sameNames(t, "board down", toolNames(t, h.connect(t, "worker1", "alpha")), fullFifteen)
}

// A found answer is remembered for a minute and looked up again after that.
func TestClassLookupIsCachedThenExpires(t *testing.T) {
	h := newClassHarness(t, classTasks)
	s := h.connect(t, "worker1", "alpha")
	_ = toolNames(t, s)
	_ = toolNames(t, s)
	if n := h.lookups.Load(); n != 1 {
		t.Fatalf("lookups inside a minute = %d, want 1", n)
	}
	h.advance(classTTL - time.Second)
	_ = toolNames(t, s)
	if n := h.lookups.Load(); n != 1 {
		t.Fatalf("lookups just inside the TTL = %d, want 1", n)
	}
	h.advance(2 * time.Second)
	_ = toolNames(t, s)
	if n := h.lookups.Load(); n != 2 {
		t.Fatalf("lookups after the TTL = %d, want 2", n)
	}
}

// A card the board does not know yet is not remembered as full.
func TestUnknownCardIsNotCached(t *testing.T) {
	h := newClassHarness(t, classTasks)
	s := h.connect(t, "nobody", "alpha")
	before := h.lookups.Load()
	_ = toolNames(t, s)
	if h.lookups.Load() == before {
		t.Fatalf("an unknown card was cached, want a lookup per request")
	}
}

func TestClassCacheIsBounded(t *testing.T) {
	h := newClassHarness(t, classTasks)
	for i := 0; i < classCacheMax+50; i++ {
		r, _ := http.NewRequest(http.MethodPost, "/", nil)
		r.Header.Set(AgentHeader, "worker1")
		r.Header.Set(RoomHeader, "room"+strings.Repeat("x", i))
		h.c.classOf(r)
	}
	if n := len(h.c.classes); n > classCacheMax {
		t.Fatalf("cache holds %d entries, cap is %d", n, classCacheMax)
	}
	h.advance(classTTL + time.Second)
	r, _ := http.NewRequest(http.MethodPost, "/", nil)
	r.Header.Set(AgentHeader, "worker1")
	h.c.classOf(r)
	if n := len(h.c.classes); n != 1 {
		t.Fatalf("expired entries kept: %d entries, want 1", n)
	}
}

// The f-020 audit still wraps a launch made from the full set.
func TestFullSetLaunchIsStillAudited(t *testing.T) {
	h := newClassHarness(t, classTasks)
	s := h.connect(t, "boss", "alpha")
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_launch",
		Arguments: map[string]any{"cwd": "/work/dir"}})
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if res.IsError {
		t.Logf("launch answered an error result: %+v", res.Content)
	}
	found := false
	for _, l := range *h.audits {
		if l.kind == "ctl-launch" && strings.Contains(l.detail, "by boss@alpha (claimed)") {
			found = true
		}
	}
	if !found {
		t.Fatalf("audit lines = %+v, want a ctl-launch by boss@alpha", *h.audits)
	}
}
