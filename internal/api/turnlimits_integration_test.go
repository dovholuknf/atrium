//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// The setting saves, reads back, is refused when it is not a limit, and the endpoint answers with the finding.
func TestTurnLimitSettingAndEndpoint(t *testing.T) {
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

	task, _, err := srv.st.Register(store.Observed{WireName: "w", Worktree: t.TempDir(), Runner: "claude"})
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if err := srv.st.RecordDelivery(&store.Delivery{TaskID: task.ID, Kind: store.DeliveryNudge, At: time.Now().UTC()}); err != nil {
			t.Fatal(err)
		}
		if _, err := srv.st.CostDeliveries(task.ID, time.Now().Add(time.Minute), store.TurnCost{Output: 5, Replies: 1}); err != nil {
			t.Fatal(err)
		}
	}
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/usage/turn-limits", nil))
	var rep TurnLimitReport
	if err := json.Unmarshal(rec.Body.Bytes(), &rep); err != nil || rec.Code != http.StatusOK {
		t.Fatalf("%d %s %v", rec.Code, rec.Body.String(), err)
	}
	if len(rep.Findings) != 1 || rep.Findings[0].Kind != store.DeliveryNudge || rep.Findings[0].Turns != 2 {
		t.Fatalf("findings %+v", rep.Findings)
	}
	if rec := settingsPost(t, srv, `{"turn_cause_limits":"off"}`); rec.Code != http.StatusOK {
		t.Fatal(rec.Body.String())
	}
	rec = httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/usage/turn-limits", nil))
	rep = TurnLimitReport{}
	_ = json.Unmarshal(rec.Body.Bytes(), &rep)
	if !rep.Off || len(rep.Findings) != 0 {
		t.Fatalf("off answered %+v", rep)
	}
}
