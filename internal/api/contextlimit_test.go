package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func putHarness(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPut, "/v1/harnesses/lim", strings.NewReader(body)))
	return rec
}

// The runners API saves and returns context_limit_k, and refuses a value out of range.
func TestTheRunnersAPISavesTheContextLimit(t *testing.T) {
	srv, _, _ := fileServer(t)
	rec := putHarness(t, srv, `{"cmd":"lim","launch_mode":"pty","context_limit_k":250}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	var saved store.Harness
	if err := json.Unmarshal(rec.Body.Bytes(), &saved); err != nil || saved.ContextLimitK != 250 {
		t.Fatalf("saved %+v, %v", saved, err)
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/harnesses", nil))
	var list struct {
		Harnesses []harnessView `json:"harnesses"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, h := range list.Harnesses {
		if h.ID == "lim" {
			found = true
			if h.ContextLimitK != 250 {
				t.Fatalf("list carries %d, want 250", h.ContextLimitK)
			}
		}
	}
	if !found {
		t.Fatal("runner not listed")
	}
	for _, bad := range []string{"5", "5000", "-1"} {
		rec = putHarness(t, srv, `{"cmd":"lim","launch_mode":"pty","context_limit_k":`+bad+`}`)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit %s gave %d, want 400", bad, rec.Code)
		}
	}
}

// A card's own limit is checked on the card patch, and empty clears it.
func TestACardLimitIsCheckedOnPatch(t *testing.T) {
	srv, st, _ := fileServer(t)
	task, _, err := st.Register(store.Observed{WireName: "c1", Worktree: "/tmp/c1", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(v string) int {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/tasks/"+task.ID,
			strings.NewReader(`{"overrides":{"context_limit_k":"`+v+`"}}`)))
		return rec.Code
	}
	if c := patch("400"); c != http.StatusOK {
		t.Fatalf("400k gave %d", c)
	}
	if k, src := ContextLimitFor(st, mustGet(t, st, task.ID)); k != 400 || src != LimitFromCard {
		t.Fatalf("limit %d from %s, want 400 from card", k, src)
	}
	for _, bad := range []string{"abc", "3", "9999"} {
		if c := patch(bad); c != http.StatusBadRequest {
			t.Fatalf("%q gave %d, want 400", bad, c)
		}
	}
	if c := patch(""); c != http.StatusOK {
		t.Fatalf("clearing gave %d", c)
	}
	if _, src := ContextLimitFor(st, mustGet(t, st, task.ID)); src == LimitFromCard {
		t.Fatal("limit still from card after clearing")
	}
}

func mustGet(t *testing.T, st *store.Store, id string) *store.Task {
	t.Helper()
	task, err := st.Get(id)
	if err != nil {
		t.Fatal(err)
	}
	return task
}

// Card, then runner, then board.
func TestTheContextLimitResolvesCardThenRunnerThenBoard(t *testing.T) {
	_, st, _ := fileServer(t)
	task, _, err := st.Register(store.Observed{WireName: "c2", Worktree: "/tmp/c2", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	check := func(wantK int, wantSrc string) {
		t.Helper()
		if k, src := ContextLimitFor(st, mustGet(t, st, task.ID)); k != wantK || src != wantSrc {
			t.Fatalf("limit %d from %s, want %d from %s", k, src, wantK, wantSrc)
		}
	}
	check(150, LimitFromBoard)
	if err := st.SetSetting(SettingContextThresholdK, "180"); err != nil {
		t.Fatal(err)
	}
	check(180, LimitFromBoard)
	h, err := st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.ContextLimitK = 300
	if _, err := st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	check(300, LimitFromRunner)
	if err := st.SetOverrides(task.ID, map[string]string{OverrideContextLimitK: "90"}); err != nil {
		t.Fatal(err)
	}
	check(90, LimitFromCard)
	if err := st.SetOverrides(task.ID, map[string]string{OverrideContextLimitK: ""}); err != nil {
		t.Fatal(err)
	}
	check(300, LimitFromRunner)
}
