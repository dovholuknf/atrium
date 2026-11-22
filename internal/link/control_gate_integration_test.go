//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The tool posts the gate to the caller's own card, verb lowered, and refuses a session atrium does not know.
func TestGateToolPostsToTheCallersOwnCard(t *testing.T) {
	var got map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "me", "wire_name": "gater", "display_title": "gater", "status": "running"}}})
		case r.URL.Path == "/v1/tasks/me/gate" && r.Method == http.MethodPost:
			_ = json.NewDecoder(r.Body).Decode(&got)
			_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "recorded": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	ctx := context.Background()

	_, out, err := c.gateHandler(ctx, ctlReq("gater", ""), gateInput{Verb: "Approve", Stage: "design", Text: "approve mock.html"})
	if err != nil {
		t.Fatal(err)
	}
	if !out.Recorded || out.Card != "me" || out.Verb != "approve" {
		t.Fatalf("out = %+v", out)
	}
	if got["verb"] != "approve" || got["stage"] != "design" || got["text"] != "approve mock.html" {
		t.Fatalf("the room got %v", got)
	}
	if _, _, err := c.gateHandler(ctx, ctlReq("", ""), gateInput{Verb: "answer", Text: "x"}); err == nil {
		t.Fatal("a session atrium does not know set a gate")
	}
}
