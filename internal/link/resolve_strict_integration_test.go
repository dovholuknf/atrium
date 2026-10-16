//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// Two cards with one title: close by title is refused and neither is closed,
// close by id closes exactly that one.
func TestCloseByAmbiguousTitleIsRefusedAndByIDWorks(t *testing.T) {
	var mu sync.Mutex
	var closed []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/close") {
			mu.Lock()
			closed = append(closed, r.URL.Path)
			mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]any{"closed": true})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "old", "wire_name": "x/m-old", "alias": "worker", "status": "running",
				"created_at": "2026-10-14T10:00:00Z"},
			{"id": "new", "wire_name": "x/m-new", "alias": "worker", "status": "running",
				"created_at": "2026-10-16T10:00:00Z"},
		}})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	ctx := context.Background()

	_, _, err := c.closeHandler(ctx, nil, closeInput{Card: "worker", Confirm: true})
	if err == nil || !strings.Contains(err.Error(), "old") || !strings.Contains(err.Error(), "new") {
		t.Fatalf("close by an ambiguous title was not refused with both cards: %v", err)
	}
	if len(closed) != 0 {
		t.Fatalf("a refused close still closed %v", closed)
	}
	if _, _, err := c.closeHandler(ctx, nil, closeInput{Card: "old", Confirm: true}); err != nil {
		t.Fatalf("close by id: %v", err)
	}
	if len(closed) != 1 || closed[0] != "/v1/tasks/old/close" {
		t.Fatalf("close by id closed %v, want only old", closed)
	}
}
