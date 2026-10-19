//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"context"

	"github.com/dovholuknf/atrium/internal/store"
)

type hubCalled struct {
	from, method, path string
	body               map[string]any
}

func TestCardSubscriptionsAsksTheHubForThisCardOnly(t *testing.T) {
	srv, st, _ := serverFor(t)
	task, _, err := st.Register(store.Observed{WireName: "orch", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var got hubCalled
	srv.HubCall = func(_ context.Context, from, method, path string, body json.RawMessage) (int, json.RawMessage, string) {
		got = hubCalled{from: from, method: method, path: path}
		_ = json.Unmarshal(body, &got.body)
		return http.StatusOK, json.RawMessage(`{"already":false}`), ""
	}
	h := srv.Handler()
	rec := post(t, h, "/v1/tasks/"+task.ID+"/subscriptions", map[string]any{"action": "subscribe", "kind": " land ", "repo": "o/r",
		"branch": "main", "card": "other~x"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"already":false`) {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if got.method != http.MethodPost || got.path != "/_hub/events/subs" || got.from != task.WireName {
		t.Fatalf("asked %+v", got)
	}
	if got.body["card"] != task.ID || got.body["kind"] != "land" || got.body["action"] != "subscribe" {
		t.Fatalf("body %+v", got.body)
	}
}

func TestCardSubscriptionsRelaysAHubRefusal(t *testing.T) {
	srv, st, _ := serverFor(t)
	task, _, _ := st.Register(store.Observed{WireName: "orch", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	srv.HubCall = func(context.Context, string, string, string, json.RawMessage) (int, json.RawMessage, string) {
		return http.StatusUnprocessableEntity, nil, "kind is one of: land"
	}
	rec := post(t, srv.Handler(), "/v1/tasks/"+task.ID+"/subscriptions", map[string]any{"action": "subscribe", "kind": "x"})
	if rec.Code != http.StatusUnprocessableEntity || !strings.Contains(rec.Body.String(), "kind is one of") {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if rec := post(t, srv.Handler(), "/v1/tasks/nope/subscriptions", map[string]any{"action": "list"}); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown card: %d", rec.Code)
	}
}

func TestCardsExistAnswersOnlyTheIdsThisRoomHolds(t *testing.T) {
	srv, st, _ := serverFor(t)
	task, _, _ := st.Register(store.Observed{WireName: "orch", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	srv.HubCall = func(context.Context, string, string, string, json.RawMessage) (int, json.RawMessage, string) {
		return http.StatusOK, nil, ""
	}
	rec := post(t, srv.Handler(), "/v1/tasks/exist", map[string]any{"ids": []string{task.ID, "gone"}})
	var out struct {
		Exist []string `json:"exist"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(out.Exist) != 1 || out.Exist[0] != task.ID {
		t.Fatalf("exist %v", out.Exist)
	}
}
