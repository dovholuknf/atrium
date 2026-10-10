//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A CARD BY ITS HANDLE ON THE ROOM'S OWN PORT: its wire name, its alias with or
// without `@`, and `alias@room` for this room. A miss lists what would have
// worked, and another room's card is the hub's to find.
func TestACardByItsHandleOnTheRoom(t *testing.T) {
	srv, st, work := fileServer(t)
	if err := st.SetTenant("sparta"); err != nil {
		t.Fatal(err)
	}
	task, _, err := st.Register(store.Observed{WireName: "rnd-director", Worktree: filepath.ToSlash(work),
		Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SetAlias(task.ID, "rnd"); err != nil {
		t.Fatal(err)
	}
	get := func(path string) (*httptest.ResponseRecorder, map[string]any) {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		var m map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &m)
		return rec, m
	}
	for _, who := range []string{"rnd", "@rnd", "RND", "rnd-director", "sparta%2Frnd-director", "rnd@sparta",
		"@rnd@SPARTA", "rnd%40sparta", task.ID} {
		rec, body := get("/v1/tasks/" + who)
		if rec.Code != http.StatusOK || body["id"] != task.ID {
			t.Errorf("%s answered %d %v", who, rec.Code, body)
			continue
		}
		if who != task.ID && (rec.Header().Get("X-Atrium-Card") != task.ID ||
			rec.Header().Get("X-Atrium-Handle") != "sparta/rnd-director@sparta") {
			t.Errorf("%s named %q %q", who, rec.Header().Get("X-Atrium-Card"), rec.Header().Get("X-Atrium-Handle"))
		}
	}
	rec, body := get("/v1/tasks/nobody/messages")
	work2, _ := body["would_work"].([]any)
	if rec.Code != http.StatusNotFound || len(work2) != 1 || work2[0] != "sparta/rnd-director (@rnd)" {
		t.Fatalf("a miss answered %d %v", rec.Code, body)
	}
	if rec, _ := get("/v1/tasks/rnd@athens"); rec.Code != http.StatusNotFound {
		t.Fatalf("another room's name answered %d", rec.Code)
	}
	// The routes whose segment is not a card are left alone.
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/tasks/pin-order", nil))
	if rec.Code == http.StatusNotFound {
		t.Fatalf("pin-order was taken for a card name")
	}
}
