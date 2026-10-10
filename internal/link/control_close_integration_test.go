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

// closeBoard lists one card and answers its close route the way a room does: a
// GET is the preview, a POST closes.
type closeBoard struct {
	method  string
	id      string
	confirm bool
	answers map[string]string
}

func (b *closeBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "w1", "wire_name": "sa36", "status": "done"},
			}})
		case strings.HasSuffix(r.URL.Path, "/close"):
			b.method = r.Method
			b.id = strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/v1/tasks/"), "/close")
			if r.Method == http.MethodPost {
				var body struct {
					Confirm bool              `json:"confirm"`
					Answers map[string]string `json:"answers"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				b.confirm, b.answers = body.Confirm, body.Answers
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"items": []string{"worktree"}})
		default:
			http.NotFound(w, r)
		}
	})
}

func TestClosePreviewsWithoutConfirmAndClosesWithIt(t *testing.T) {
	board := &closeBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.closeHandler(context.Background(), ctlReq("orch", "beta"), closeInput{Card: "sa36"})
	if err != nil {
		t.Fatalf("preview: %v", err)
	}
	if board.method != http.MethodGet || board.id != "w1" {
		t.Fatalf("preview reached the room as %s %q, want GET w1", board.method, board.id)
	}
	if out["items"] == nil || out["handle"] != "sa36" {
		t.Errorf("out = %v, want the room's preview and the handle", out)
	}

	_, _, err = c.closeHandler(context.Background(), ctlReq("orch", "beta"),
		closeInput{Card: "sa36", Confirm: true, Answers: map[string]string{"1": "stash"}})
	if err != nil {
		t.Fatalf("close: %v", err)
	}
	if board.method != http.MethodPost || !board.confirm || board.answers["1"] != "stash" {
		t.Fatalf("close reached the room as %s confirm=%v answers=%v", board.method, board.confirm, board.answers)
	}
}
