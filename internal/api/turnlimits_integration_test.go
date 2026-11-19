//go:build integration

package api

import (
	"net/http"
	"testing"
)

// The setting saves, reads back and is refused when it is not a limit.
func TestTurnLimitSetting(t *testing.T) {
	srv, _, _ := fileServer(t)
	if out := settingsGet(t, srv); out["turn_cause_limit_default"] == nil {
		t.Fatalf("no default in %v", out)
	}
	if rec := settingsPost(t, srv, `{"turn_cause_limits":"lots"}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("a bad limit answered %d", rec.Code)
	}
	if rec := settingsPost(t, srv, `{"turn_cause_limits":"{\"turns\":1,\"tokens\":0}"}`); rec.Code != http.StatusOK {
		t.Fatalf("a good limit answered %d: %s", rec.Code, rec.Body.String())
	}
	if got := settingsGet(t, srv)["turn_cause_limits"]; got != `{"turns":1,"tokens":0}` {
		t.Fatalf("read back %v", got)
	}
}
