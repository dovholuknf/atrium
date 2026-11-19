package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNewOnALocalRepoLeavesTheRunnerToTheRoom(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		_, _ = w.Write([]byte(`{"id":"c1","runner":"codex"}`))
	}))
	defer srv.Close()
	if err := startNewLocal(t.TempDir(), "b", newOpts{boardURL: srv.URL, runner: " "}); err != nil {
		t.Fatal(err)
	}
	if h, ok := got["harness"]; ok && h != "" {
		t.Fatalf("harness = %v, want the room's default", h)
	}
}
