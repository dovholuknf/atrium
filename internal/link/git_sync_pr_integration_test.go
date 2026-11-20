//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_git_sync with no room is the calling card's pull request: its room is asked to sync that card, and the
// room's answer comes back under `pr`. With no card calling, no room is still refused.
func TestGitSyncWithNoRoomSyncsTheCallersPullRequest(t *testing.T) {
	var asked atomic.Int32
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "c1", "wire_name": "reviewer", "status": "working",
					"tags": []string{"atrium:subagent", "origin:agent"}}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/c1/pr-sync":
			asked.Add(1)
			_ = json.NewEncoder(w).Encode(map[string]any{"card": "c1", "pr": "github.com/o/r#7", "branch": "feat/x",
				"state": "fast-forward", "commits": []string{"abc1234 second change"}, "text": "feat/x fast-forwarded"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL, client: board.Client(), audit: func(string, string, string) {}}
	ts := httptest.NewServer(c.handler())
	defer ts.Close()
	connect := func(rt http.RoundTripper) *mcp.ClientSession {
		cl := mcp.NewClient(&mcp.Implementation{Name: "t", Version: "v0"}, nil)
		s, err := cl.Connect(context.Background(), &mcp.StreamableClientTransport{
			Endpoint: ts.URL, HTTPClient: &http.Client{Transport: rt}, DisableStandaloneSSE: true, MaxRetries: -1}, nil)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}

	s := connect(headerTransport{"reviewer", "alpha"})
	defer s.Close()
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_sync", Arguments: map[string]any{}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if asked.Load() != 1 || !strings.Contains(string(raw), `"state":"fast-forward"`) ||
		!strings.Contains(string(raw), "abc1234 second change") {
		t.Fatalf("asked %d, answered %s", asked.Load(), raw)
	}

	anon := connect(http.DefaultTransport)
	defer anon.Close()
	res, err = anon.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_git_sync", Arguments: map[string]any{}})
	if err == nil && (res == nil || !res.IsError) {
		t.Fatal("no room and no card was not refused")
	}
	if asked.Load() != 1 {
		t.Fatal("a call with no card reached the room")
	}
}
