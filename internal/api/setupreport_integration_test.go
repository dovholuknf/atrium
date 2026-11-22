//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/setupcheck"
)

func TestSetupReportHoldsTheEngineAndTheRunnerRows(t *testing.T) {
	srv, st, _ := fileServer(t)
	_ = st
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/setup", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var rep setupcheck.Report
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil {
		t.Fatal(err)
	}
	ids := map[string]bool{}
	for _, c := range rep.Checks {
		ids[c.ID] = true
	}
	for _, want := range []string{"account.standard", "binary.installed", "tool.git"} {
		if !ids[want] {
			t.Errorf("no %s row in %v", want, ids)
		}
	}
	runner := false
	for id := range ids {
		runner = runner || strings.HasPrefix(id, "runner.")
	}
	if !runner {
		t.Errorf("no runner row for an enabled runner with an adapter: %v", ids)
	}
}
