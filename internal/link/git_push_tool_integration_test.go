//go:build integration

package link

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_git_push pushes for the CALLER's own card: the room does the push, so the tool names the card and the branch
// and nothing else, and a branch that is not a plain branch is refused before the room is asked.
func TestGitPushToolAsksTheRoomToPushTheCallersOwnCard(t *testing.T) {
	var mu sync.Mutex
	var posts []string
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "c1", "wire_name": "doer", "status": "working", "tags": []string{"atrium:subagent"}}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/c1/git-push":
			b, _ := io.ReadAll(r.Body)
			mu.Lock()
			posts = append(posts, string(b))
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"card": "c1", "branch": "fix/x", "report": "To hub\n*\trefs/heads/fix/x\t[new branch]"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL, client: board.Client(), audit: func(string, string, string) {}}
	ts := httptest.NewServer(c.handler())
	defer ts.Close()
	cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "v0"}, nil)
	s, err := cl.Connect(context.Background(), &mcp.StreamableClientTransport{
		Endpoint: ts.URL, HTTPClient: &http.Client{Transport: headerTransport{"doer", "alpha"}},
		DisableStandaloneSSE: true, MaxRetries: -1}, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()

	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_push",
		Arguments: map[string]any{"branch": "fix/x"}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	mu.Lock()
	got := append([]string{}, posts...)
	mu.Unlock()
	if len(got) != 1 || got[0] != `{"branch":"fix/x"}` {
		t.Fatalf("the room was asked %q", got)
	}
	for _, bad := range []string{"+fix/x", "fix/x:main", "--force", "refs/tags/v1", "HEAD", ""} {
		res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_push",
			Arguments: map[string]any{"branch": bad}})
		if err == nil && (res == nil || !res.IsError) {
			t.Errorf("%q was not refused", bad)
		}
	}
	mu.Lock()
	defer mu.Unlock()
	if len(posts) != 1 {
		t.Fatalf("a refused branch reached the room: %q", posts)
	}
}
