//go:build integration

package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestSettingsLeanWorkerGatewayIsANameNotAURL(t *testing.T) {
	srv, st, _ := fileServer(t)
	if got := settingsGet(t, srv)["lean_worker_gateway"]; got != "" {
		t.Fatalf("default = %v, want empty", got)
	}
	for _, bad := range []string{"http://127.0.0.1:8089/mcp", "a b", "host:8089"} {
		body, _ := json.Marshal(map[string]string{"lean_worker_gateway": bad})
		if rec := settingsPost(t, srv, string(body)); rec.Code != http.StatusBadRequest {
			t.Fatalf("%q answered %d, want 400", bad, rec.Code)
		}
	}
	if rec := settingsPost(t, srv, `{"lean_worker_gateway":" mercurius-worker "}`); rec.Code != 200 {
		t.Fatalf("a name answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["lean_worker_gateway"]; got != "mercurius-worker" || st.LeanWorkerGateway() != "mercurius-worker" {
		t.Fatalf("read back %v", got)
	}
}

// The card's details say which mercurius it got.
func TestSettingsLeanWorkerGatewayShowsOnTheCard(t *testing.T) {
	srv, st, _ := fileServer(t)
	get := func(id string) string {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/tasks/"+id, nil))
		var out map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatal(err)
		}
		s, _ := out["mercurius"].(string)
		return s
	}
	mk := func(name string, tags ...string) string {
		c, _, err := st.Register(store.Observed{WireName: name, Worktree: t.TempDir()})
		if err != nil {
			t.Fatal(err)
		}
		if err := st.SetTags(c.ID, tags); err != nil {
			t.Fatal(err)
		}
		return c.ID
	}
	narrow := mk("narrow", "atrium:lean")
	wide := mk("wide", "atrium:lean", "atrium:mcp:mercurius")
	if get(narrow) != "" {
		t.Fatal("the field shows with the setting off")
	}
	if err := st.SetSetting(store.SettingLeanWorkerGateway, "mercurius-worker"); err != nil {
		t.Fatal(err)
	}
	if got := get(narrow); got != "mercurius: mercurius-worker" {
		t.Fatalf("narrow card = %q", got)
	}
	if got := get(wide); got != "mercurius: mercurius (wide)" {
		t.Fatalf("wide card = %q", got)
	}
}

func TestSettingsLeanWorkerGatewayUnknownNameIsRefusedOnSave(t *testing.T) {
	srv, st, _ := fileServer(t)
	old := CheckLeanGateway
	t.Cleanup(func() { CheckLeanGateway = old })
	CheckLeanGateway = func(name string) error {
		if name == "mercurius-worker" {
			return nil
		}
		return fmt.Errorf("no MCP server named %s in this runner's config. it has: mercurius, mercurius-worker", name)
	}
	rec := settingsPost(t, srv, `{"lean_worker_gateway":"typo"}`)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "it has: mercurius, mercurius-worker") {
		t.Fatalf("answered %d: %s", rec.Code, rec.Body.String())
	}
	if st.LeanWorkerGateway() != "" {
		t.Fatal("a refused name was stored")
	}
	if rec := settingsPost(t, srv, `{"lean_worker_gateway":"mercurius-worker"}`); rec.Code != 200 {
		t.Fatalf("a good name answered %d", rec.Code)
	}
}
