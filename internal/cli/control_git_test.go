package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_git_push and atrium_git_url on the stdio server, against a fake room board, called through the server
// controlServer builds, so a tool that is not registered fails here.

type gitBoard struct {
	mu     sync.Mutex
	old    bool // a room older than both routes
	pushed []string
	looked []string
	answer map[string]any
	ci     []map[string]any
	ciErr  string
}

func (b *gitBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/tasks" && r.Method == http.MethodGet:
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "c1", "wire_name": "sa1", "status": "running"}}})
	case r.URL.Path == "/v1/tasks/c1/git-push" && !b.old:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.pushed = append(b.pushed, body["branch"])
		if body["branch"] == "refused/x" {
			w.WriteHeader(http.StatusConflict)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "the push did not land:\nremote: atrium: no"})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"card": "c1", "branch": body["branch"], "report": "To hub\n*\trefs/heads/x"})
	case r.URL.Path == "/v1/hub/git/url" && !b.old:
		b.looked = append(b.looked, r.URL.RawQuery)
		_ = json.NewEncoder(w).Encode(b.answer)
	case r.URL.Path == "/v1/hub/ci" && !b.old:
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.ci = append(b.ci, body)
		if b.ciErr != "" {
			w.WriteHeader(http.StatusBadGateway)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": b.ciErr})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"kind": "github", "runs": []map[string]any{{"id": 7, "workflow": "ci"}}})
	case r.URL.Path == "/v1/hub-remote":
		_ = json.NewEncoder(w).Encode(map[string]string{"base": "http://127.0.0.1:7777/git/"})
	default:
		w.Header().Set("Content-Type", "text/plain")
		http.NotFound(w, r)
	}
}

func gitSession(t *testing.T, b *gitBoard) *mcp.ClientSession {
	t.Helper()
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	t.Setenv("ATRIUM_AGENT_NAME", "sa1")
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	ss, err := controlServer().Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ss.Close() })
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cs.Close() })
	return cs
}

// callTool answers the tool's structured result, or the text of its error.
func callTool(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (map[string]any, string) {
	t.Helper()
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: %v", tool, err)
	}
	if res.IsError {
		var msg []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				msg = append(msg, tc.Text)
			}
		}
		return nil, strings.Join(msg, " ")
	}
	var out map[string]any
	raw, _ := json.Marshal(res.StructuredContent)
	_ = json.Unmarshal(raw, &out)
	return out, ""
}

func TestStdioControlHasBothGitTools(t *testing.T) {
	cs := gitSession(t, &gitBoard{})
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	have := map[string]bool{}
	for _, tl := range res.Tools {
		have[tl.Name] = true
	}
	for _, n := range []string{"atrium_git_push", "atrium_git_url", "atrium_git_clone", "atrium_ci"} {
		if !have[n] {
			t.Errorf("the stdio control has no %s", n)
		}
	}
}

func TestStdioGitPushPushesTheCallersOwnCardAndTakesOnlyABranch(t *testing.T) {
	b := &gitBoard{}
	cs := gitSession(t, b)
	out, msg := callTool(t, cs, "atrium_git_push", map[string]any{"branch": " claude/x "})
	if msg != "" || out["card"] != "c1" || out["branch"] != "claude/x" || !strings.Contains(out["report"].(string), "refs/heads/x") {
		t.Fatalf("push = %v %q", out, msg)
	}
	// The room's refusal is the answer, in its words.
	if _, msg = callTool(t, cs, "atrium_git_push", map[string]any{"branch": "refused/x"}); !strings.Contains(msg, "remote: atrium: no") {
		t.Fatalf("a refusal: %q", msg)
	}
	// Nothing but a plain branch reaches the room.
	for _, bad := range []string{"+x", "x:main", "--force", "refs/heads/x", ""} {
		if _, msg = callTool(t, cs, "atrium_git_push", map[string]any{"branch": bad}); msg == "" {
			t.Errorf("%q was pushed", bad)
		}
	}
	if len(b.pushed) != 2 {
		t.Fatalf("the room was asked to push %v", b.pushed)
	}
	// A session atrium did not launch has no card to push for.
	t.Setenv("ATRIUM_AGENT_NAME", "")
	if _, msg = callTool(t, cs, "atrium_git_push", map[string]any{"branch": "claude/x"}); !strings.Contains(msg, "ATRIUM_AGENT_NAME") {
		t.Fatalf("no card: %q", msg)
	}
}

func TestStdioGitURLPutsTheHubsURLsOnThisRoomsForwarder(t *testing.T) {
	b := &gitBoard{answer: map[string]any{"state": "found", "repo": "github/o/r", "branch": "claude/x",
		"branches": []map[string]any{{"name": "claude/x", "sources": []map[string]any{
			{"source": "hub", "url": "http://hub/git/hub/github/o/r.git", "sha": strings.Repeat("a", 40)},
			{"source": "room", "room": "sg4", "online": true, "url": "http://hub/git/room/sg4/github/o/r.git", "sha": strings.Repeat("b", 40)},
		}}}}}
	cs := gitSession(t, b)
	out, msg := callTool(t, cs, "atrium_git_url", map[string]any{"repo": "o/r", "branch": "claude/x"})
	if msg != "" {
		t.Fatal(msg)
	}
	if len(b.looked) != 1 || !strings.Contains(b.looked[0], "repo=o%2Fr") || !strings.Contains(b.looked[0], "branch=claude%2Fx") {
		t.Fatalf("the room was asked %v", b.looked)
	}
	srcs := out["branches"].([]any)[0].(map[string]any)["sources"].([]any)
	hub, room := srcs[0].(map[string]any), srcs[1].(map[string]any)
	if hub["url"] != "http://127.0.0.1:7777/git/hub/github/o/r.git" || hub["sha"] != strings.Repeat("a", 40) {
		t.Fatalf("the hub source: %v", hub)
	}
	if u, _ := room["url"].(string); u != "" {
		t.Fatalf("a room's work in progress got a URL: %v", room)
	}
	if !strings.Contains(out["text"].(string), "http://127.0.0.1:7777/git/hub/github/o/r.git") {
		t.Fatalf("the text: %v", out["text"])
	}
	if _, msg = callTool(t, cs, "atrium_git_url", map[string]any{"repo": " "}); msg == "" {
		t.Fatal("no repository was looked up")
	}
}

func TestStdioGitToolsSayAnOlderRoomIsOlder(t *testing.T) {
	cs := gitSession(t, &gitBoard{old: true})
	if _, msg := callTool(t, cs, "atrium_git_push", map[string]any{"branch": "claude/x"}); !strings.Contains(msg, "predates atrium_git_push") {
		t.Fatalf("push: %q", msg)
	}
	if _, msg := callTool(t, cs, "atrium_git_url", map[string]any{"repo": "o/r"}); !strings.Contains(msg, "predates atrium_git_url") {
		t.Fatalf("url: %q", msg)
	}
}
