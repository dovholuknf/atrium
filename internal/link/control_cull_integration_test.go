//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// cullBoard lists a worker and its launcher, and answers a cull the way a room
// does, or with a bare 404 like a room older than the endpoint.
type cullBoard struct {
	old     bool
	culled  string
	gotInto string
}

func (b *cullBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "w1", "wire_name": "sa36", "status": "needs-input"},
				{"id": "o1", "wire_name": "orch", "status": "working"},
			}})
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/cull") && !b.old:
			b.culled = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/cull")
			var body struct {
				Into string `json:"into"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			b.gotInto = body.Into
			_ = json.NewEncoder(w).Encode(map[string]any{
				"card": "w1", "exited": true, "branch": "claude/sa36", "into": "claude/main",
				"worktree_removed": true, "branch_deleted": true,
			})
		default:
			http.NotFound(w, r)
		}
	})
}

func TestCullForwardsToTheRoom(t *testing.T) {
	board := &cullBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.cullHandler(context.Background(), ctlReq("orch", "beta"),
		cullInput{Card: "sa36", Into: "claude/main"})
	if err != nil {
		t.Fatalf("cull: %v", err)
	}
	if board.culled != "w1" || board.gotInto != "claude/main" {
		t.Fatalf("room got cull of %q into %q, want w1 into claude/main", board.culled, board.gotInto)
	}
	if !out.Exited || !out.WorktreeRemoved || !out.BranchDeleted || out.Handle != "sa36" {
		t.Errorf("out = %+v, want the room's answer carried through", out)
	}
}

// A worker's own done is not the acceptance.
func TestCullRefusesTheCallerItself(t *testing.T) {
	board := &cullBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err := c.cullHandler(context.Background(), ctlReq("sa36", "beta"), cullInput{Card: "w1"})
	if err == nil || !strings.Contains(err.Error(), "cannot cull itself") {
		t.Fatalf("err = %v, want a self-cull refused", err)
	}
	if board.culled != "" {
		t.Error("a self-cull reached the room")
	}
}

func TestCullOnAnOlderRoomSaysSo(t *testing.T) {
	board := &cullBoard{old: true}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, _, err := c.cullHandler(context.Background(), ctlReq("orch", "beta"), cullInput{Card: "sa36"})
	if err == nil || !strings.Contains(err.Error(), "predates cull (needs "+cullSince+")") {
		t.Fatalf("err = %v, want the older room named", err)
	}
}
