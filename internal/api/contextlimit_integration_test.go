//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strconv"
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

// Card, then the hub's limit for the harness, then the built-in claude 200k. The runner column is not read.
func TestTheContextLimitResolvesCardThenHubThenDefault(t *testing.T) {
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
	check(200, LimitFromDefault)
	h, err := st.Harness("claude")
	if err != nil {
		t.Fatal(err)
	}
	h.ContextLimitK = 300
	if _, err := st.SaveHarness(*h); err != nil {
		t.Fatal(err)
	}
	check(200, LimitFromDefault)
	if err := st.SetSetting(store.SettingContextLimits, `{"claude":250}`); err != nil {
		t.Fatal(err)
	}
	check(250, LimitFromHub)
	if err := st.SetOverrides(task.ID, map[string]string{OverrideContextLimitK: "90"}); err != nil {
		t.Fatal(err)
	}
	check(90, LimitFromCard)
	if err := st.SetOverrides(task.ID, map[string]string{OverrideContextLimitK: ""}); err != nil {
		t.Fatal(err)
	}
	check(250, LimitFromHub)
	// A harness with no entry and no claude in it has no limit.
	other, _, err := st.Register(store.Observed{WireName: "c3", Worktree: "/tmp/c3", Runner: "shell"})
	if err != nil {
		t.Fatal(err)
	}
	if k, _ := ContextLimitFor(st, mustGet(t, st, other.ID)); k != 0 {
		t.Fatalf("a shell card has a limit of %dk", k)
	}
}

// The per-card switch is checked on the patch, and off turns cycling off.
func TestACardCycleSwitchIsCheckedOnPatch(t *testing.T) {
	srv, st, _ := fileServer(t)
	task, _, err := st.Register(store.Observed{WireName: "c4", Worktree: "/tmp/c4", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(v string) int {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/tasks/"+task.ID,
			strings.NewReader(`{"overrides":{"context_cycle":"`+v+`"}}`)))
		return rec.Code
	}
	if !ContextCycleOn(mustGet(t, st, task.ID)) {
		t.Fatal("a new card does not cycle")
	}
	if c := patch("off"); c != http.StatusOK || ContextCycleOn(mustGet(t, st, task.ID)) {
		t.Fatalf("off gave %d and the card still cycles", c)
	}
	if c := patch("maybe"); c != http.StatusBadRequest {
		t.Fatalf("junk gave %d", c)
	}
	if c := patch(""); c != http.StatusOK || !ContextCycleOn(mustGet(t, st, task.ID)) {
		t.Fatalf("clearing gave %d and the card does not cycle", c)
	}
}

// The per-harness limits and the handoff directory save, read back, and refuse junk.
func TestTheContextCycleSettingsSaveAndReadBack(t *testing.T) {
	srv, _, _ := fileServer(t)
	out := settingsGet(t, srv)
	if lim, _ := out["context_limits"].(map[string]any); lim["claude"] != float64(200) {
		t.Fatalf("the default reads %v", out["context_limits"])
	}
	if rec := settingsPost(t, srv, `{"context_limits":{"claude":180,"codex":300}}`); rec.Code != http.StatusOK {
		t.Fatalf("saving answered %d: %s", rec.Code, rec.Body.String())
	}
	out = settingsGet(t, srv)
	if lim, _ := out["context_limits"].(map[string]any); lim["claude"] != float64(180) || lim["codex"] != float64(300) {
		t.Fatalf("read back %v", out["context_limits"])
	}
	if rec := settingsPost(t, srv, `{"context_limits":{"claude":5}}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("5k answered %d", rec.Code)
	}
	if rec := settingsPost(t, srv, `{"context_handoff_dir":"relative"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a relative directory answered %d", rec.Code)
	}
	dir := t.TempDir()
	if rec := settingsPost(t, srv, `{"context_handoff_dir":`+strconv.Quote(dir)+`}`); rec.Code != http.StatusOK {
		t.Fatalf("a real directory answered %d: %s", rec.Code, rec.Body.String())
	}
	if out = settingsGet(t, srv); out["context_handoff_dir"] != filepath.Clean(dir) {
		t.Fatalf("the directory reads %v", out["context_handoff_dir"])
	}
}

// A runner that compacts at 220k leaves room for a 200k limit and no more: a card limit above that is refused at
// PATCH naming the ceiling, and a hub limit above it is cut and says so.
func TestTheRunnersCompactionPointIsACeilingOnTheLimit(t *testing.T) {
	srv, st, _ := fileServer(t)
	RunnerCompactAtK = func(*store.Task) int { return 220 }
	t.Cleanup(func() { RunnerCompactAtK = nil })
	task, _, err := st.Register(store.Observed{WireName: "c9", Worktree: "/tmp/c9", Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	patch := func(v string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPatch, "/v1/tasks/"+task.ID,
			strings.NewReader(`{"overrides":{"context_limit_k":"`+v+`"}}`)))
		return rec
	}
	if c := RunnerCeilingK(task); c != 200 {
		t.Fatalf("ceiling %d, want 200", c)
	}
	if rec := patch("200"); rec.Code != http.StatusOK {
		t.Fatalf("200k gave %d %s", rec.Code, rec.Body)
	}
	rec := patch("900")
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "ceiling of 200k") ||
		!strings.Contains(rec.Body.String(), "220k") {
		t.Fatalf("900k gave %d %s, want a 400 naming the ceiling", rec.Code, rec.Body)
	}
	if l := ContextLimitOf(st, mustGet(t, st, task.ID)); l.K != 200 || l.From != LimitFromCard || l.OwnK != 200 {
		t.Fatalf("refused value changed the card: %+v", l)
	}
	if rec := patch(""); rec.Code != http.StatusOK {
		t.Fatalf("clearing gave %d", rec.Code)
	}
	// The hub's limit cannot be refused at a card PATCH, so it is capped and the limit says what it was.
	if err := st.SetSetting(store.SettingContextLimits, `{"claude":900}`); err != nil {
		t.Fatal(err)
	}
	l := ContextLimitOf(st, mustGet(t, st, task.ID))
	if l.K != 200 || l.From != LimitFromRunner || l.WantedK != 900 || l.WantedFrom != LimitFromHub || l.CeilingK != 200 {
		t.Fatalf("hub 900k under a 220k runner gave %+v, want 200 from runner, wanted 900 from hub", l)
	}
	// A card whose runner has no known compaction point is not held to anything.
	RunnerCompactAtK = func(*store.Task) int { return 0 }
	if l := ContextLimitOf(st, mustGet(t, st, task.ID)); l.K != 900 || l.From != LimitFromHub || l.CeilingK != 0 {
		t.Fatalf("no known compaction point gave %+v", l)
	}
}
