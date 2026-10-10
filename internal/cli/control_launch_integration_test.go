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
	"testing"
)

// The stdio atrium_launch takes the launch options and warns, as the hub's does,
// when the room hands back a card that does not carry them. The sentence comes
// from internal/link so the two cannot drift.
func TestStdioLaunchWarnsWhenARoomDropsItsOptions(t *testing.T) {
	echo := true
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			Model  string   `json:"model"`
			Effort string   `json:"effort"`
			Args   []string `json:"args"`
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		card := map[string]any{"id": "card1", "wire_name": "kid"}
		if echo {
			card["model"], card["effort"] = got.Model, got.Effort
			card["launch_args"] = got.Args
			card["launch_env_keys"] = []string{"K"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	in := LaunchInput{Cwd: "/w", Model: "claude-haiku-4-5-20251001", Effort: "low",
		Args: []string{"--x"}, Env: map[string]string{"K": "v"}}

	_, out, err := launchHandler(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if out.Model != in.Model || out.Effort != "low" || strings.Contains(out.Note, "WARNING") {
		t.Fatalf("a room that applied them: out %+v", out)
	}

	echo = false
	_, out, err = launchHandler(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Note, "model, effort, args, env were NOT applied") {
		t.Fatalf("an older room's silence was passed on as success: %q", out.Note)
	}
}

// lean_agents goes to the room as given, and a room that ignores it (its card
// carries no atrium:agent: tag) is named in the warning.
func TestStdioLaunchSendsLeanAgentsAndWarnsWhenARoomDropsThem(t *testing.T) {
	applied := true
	var sent []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var got struct {
			LeanAgents []string `json:"lean_agents"`
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		sent = got.LeanAgents
		card := map[string]any{"id": "card1", "wire_name": "kid"}
		if applied {
			card["tags"] = []string{"atrium:lean", "atrium:agent:codebase-steward"}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(card)
	}))
	defer srv.Close()
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	in := LaunchInput{Cwd: "/w", LeanAgents: []string{"codebase-steward"}}

	_, out, err := launchHandler(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if len(sent) != 1 || sent[0] != "codebase-steward" || strings.Contains(out.Note, "WARNING") {
		t.Fatalf("sent %q, note %q", sent, out.Note)
	}
	applied = false
	_, out, err = launchHandler(context.Background(), nil, in)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.Note, "lean_agents was NOT applied") {
		t.Fatalf("an older room's silence was passed on as success: %q", out.Note)
	}
}
