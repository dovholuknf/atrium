//go:build integration

package api

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestCardMoveAsksTheHubToMoveThisCardAndFollowsIt(t *testing.T) {
	srv, st, _ := serverFor(t)
	task, _, err := st.Register(store.Observed{WireName: "orch", Worktree: filepath.ToSlash(t.TempDir()), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	var got []hubCalled
	srv.HubCall = func(_ context.Context, from, method, path string, body json.RawMessage) (int, json.RawMessage, string) {
		c := hubCalled{from: from, method: method, path: path}
		_ = json.Unmarshal(body, &c.body)
		got = append(got, c)
		return http.StatusOK, json.RawMessage(`{"move_id":"mv-1","state":"running"}`), ""
	}
	h := srv.Handler()
	rec := post(t, h, "/v1/tasks/"+task.ID+"/move", map[string]any{"to": " sg4 ", "wip": true, "card": "other"})
	if rec.Code != http.StatusOK {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	if len(got) != 1 || got[0].method != http.MethodPost || got[0].path != "/_hub/move" || got[0].from != task.WireName {
		t.Fatalf("asked %+v", got)
	}
	if got[0].body["card"] != task.ID || got[0].body["to"] != "sg4" || got[0].body["wip"] != true {
		t.Fatalf("body %+v", got[0].body)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.ID+"/move?move=mv-1", nil))
	if rec.Code != http.StatusOK || len(got) != 2 || got[1].method != http.MethodGet || got[1].path != "/_hub/move/mv-1" {
		t.Fatalf("%d %+v", rec.Code, got)
	}
	if rec := post(t, h, "/v1/tasks/"+task.ID+"/move", map[string]any{"undo": "mv-1"}); rec.Code != http.StatusOK ||
		len(got) != 3 || got[2].method != http.MethodPost || got[2].path != "/_hub/move/mv-1/undo" {
		t.Fatalf("undo: %d %+v", rec.Code, got)
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+task.ID+"/move", nil))
	if rec.Code != http.StatusBadRequest || len(got) != 3 {
		t.Fatalf("no move id: %d, %d asks", rec.Code, len(got))
	}
}
