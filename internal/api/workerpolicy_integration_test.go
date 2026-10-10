//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func settingsWith(t *testing.T, srv *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/settings", strings.NewReader(body)))
	return rec
}

func policyRead(t *testing.T, srv *Server) store.WorkerPolicy {
	t.Helper()
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/settings", nil))
	var got struct {
		WorkerPolicy store.WorkerPolicy `json:"worker_policy"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	return got.WorkerPolicy
}

// Both worker settings are off on a fresh room, saved and read back, and a bad one is refused.
func TestTheWorkerSettingsAreOffThenSavedAndChecked(t *testing.T) {
	srv, st, _ := fileServer(t)
	if p := policyRead(t, srv); p != (store.WorkerPolicy{}) {
		t.Fatalf("a fresh room has %+v, want both off", p)
	}
	if rec := settingsWith(t, srv, `{"worker_policy":{"model":"sonnet","budget_usd":15}}`); rec.Code != http.StatusOK {
		t.Fatalf("save: %d %s", rec.Code, rec.Body)
	}
	if p := policyRead(t, srv); p.Model != "sonnet" || p.BudgetUSD != 15 || st.WorkerPolicy() != p {
		t.Fatalf("read back %+v", p)
	}
	for _, bad := range []string{`{"budget_usd":-1}`, `{"model":"not a model"}`} {
		if rec := settingsWith(t, srv, `{"worker_policy":`+bad+`}`); rec.Code != http.StatusBadRequest {
			t.Fatalf("%s gave %d, want 400", bad, rec.Code)
		}
	}
	if p := policyRead(t, srv); p.BudgetUSD != 15 {
		t.Fatalf("a refused write changed it to %+v", p)
	}
	if rec := settingsWith(t, srv, `{"worker_policy":{"model":"","budget_usd":0}}`); rec.Code != http.StatusOK {
		t.Fatalf("off: %d %s", rec.Code, rec.Body)
	}
	if p := policyRead(t, srv); p != (store.WorkerPolicy{}) {
		t.Fatalf("off left %+v", p)
	}
}
