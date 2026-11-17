//go:build integration

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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type capBoard struct {
	mu      sync.Mutex
	asked   []string
	launch  []map[string]any
	passing []string
}

func (b *capBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch r.URL.Path {
	case "/v1/hub/ask":
		var in struct{ Method, Path string }
		_ = json.NewDecoder(r.Body).Decode(&in)
		b.asked = append(b.asked, in.Method+" "+in.Path)
		at := time.Now().UTC().Format(time.RFC3339)
		res := []map[string]any{}
		for _, room := range []string{"a", "b"} {
			state := "fail"
			for _, p := range b.passing {
				if p == room {
					state = "pass"
				}
			}
			res = append(res, map[string]any{"room": room, "cap": "ssh:x", "state": state, "reason": "timeout", "at": at})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"caps":    []map[string]any{{"id": "ssh:x", "kind": "reach"}},
			"rooms":   []map[string]any{{"name": "a", "online": true}, {"name": "b", "online": true}},
			"results": res,
		})
	case "/v1/peers/launch":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.launch = append(b.launch, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"card": "c9", "handle": "h9", "title": "t", "status": "running"})
	default:
		http.NotFound(w, r)
	}
}

func capSession(t *testing.T, b *capBoard) *mcp.ClientSession {
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
	t.Setenv("ATRIUM_ROOM", "a")
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

func TestStdioCapabilitiesAsksTheHubThroughTheRoom(t *testing.T) {
	b := &capBoard{passing: []string{"b"}}
	cs := capSession(t, b)
	out, msg := callTool(t, cs, "atrium_capabilities", map[string]any{"action": "which", "need": []string{"ssh:x"}})
	if msg != "" {
		t.Fatal(msg)
	}
	rooms, _ := out["rooms"].([]any)
	if len(rooms) != 1 || rooms[0].(map[string]any)["room"] != "b" || rooms[0].(map[string]any)["ok"] != true {
		t.Fatalf("which = %v", out)
	}
	if len(b.asked) != 1 || b.asked[0] != "GET /_hub/capabilities" {
		t.Errorf("asked = %v", b.asked)
	}
	if _, msg := callTool(t, cs, "atrium_capabilities", map[string]any{"action": "which"}); !strings.Contains(msg, "name what is needed") {
		t.Errorf("msg = %q", msg)
	}
}

func TestStdioLaunchWithNeedsGoesToARoomThatPasses(t *testing.T) {
	b := &capBoard{passing: []string{"b"}}
	cs := capSession(t, b)
	out, msg := callTool(t, cs, "atrium_launch", map[string]any{"cwd": "/w", "title": "t", "needs": []string{"ssh:x"}})
	if msg != "" {
		t.Fatalf("%s %v", msg, out)
	}
	if len(b.launch) != 1 || b.launch[0]["room"] != "b" {
		t.Fatalf("the launch went to %v", b.launch)
	}
	if n, _ := b.launch[0]["needs"].([]any); len(n) != 1 || n[0] != "ssh:x" {
		t.Errorf("needs did not travel: %v", b.launch[0]["needs"])
	}
}

func TestStdioLaunchWithNeedsNoRoomPassesIsRefusedWithReasons(t *testing.T) {
	b := &capBoard{}
	cs := capSession(t, b)
	_, msg := callTool(t, cs, "atrium_launch", map[string]any{"cwd": "/w", "title": "t", "needs": []string{"ssh:x"}})
	if !strings.Contains(msg, "no room can take this launch") || !strings.Contains(msg, "a: ssh:x fail: timeout") {
		t.Errorf("msg = %q", msg)
	}
	if len(b.launch) != 0 {
		t.Errorf("a launch was posted: %v", b.launch)
	}
}
