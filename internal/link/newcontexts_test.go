package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDeployClickWaitsForANewContextAndNamesTheCard(t *testing.T) {
	r := newReadyRepo(t)
	base := r.commit("base", map[string]string{"internal/a.go": "a"})
	code := r.commit("code", map[string]string{"internal/hubstore/a.go": "1"})
	r.commit("Review\n\nAtrium-Verdict: hub-ok "+code, map[string]string{"docs/backlog/rt/r-new-review-a.md": "ok"})
	tip := r.git("rev-parse", "HEAD")
	p, started := readyProxy(t, r, base, scriptFile(t))
	runs := []newContextRun{{Room: "sg4", Card: "01a0f2da", Name: "orchestrator", Step: "clear", N: 2}}
	p.deployReady().clearing = func(context.Context) []newContextRun { return runs }

	body := `{"tip":"` + tip + `"}`
	rec := postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", body))
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "orchestrator@sg4 (step 2 of 3, clear)") {
		t.Fatalf("click over a clear = %d %s", rec.Code, rec.Body)
	}
	if len(*started) != 0 {
		t.Fatal("a deploy was started over a new context")
	}

	runs = nil
	rec = postDeploy(p, loopbackReq(http.MethodPost, "/_hub/deploy-ready/deploy", body))
	if rec.Code != http.StatusAccepted || len(*started) != 1 {
		t.Fatalf("click once the clear is over = %d %s", rec.Code, rec.Body)
	}
}

func TestNewContextsRouteAnswersEmptyWithNoRooms(t *testing.T) {
	p, _ := readyProxy(t, newReadyRepo(t), "x", scriptFile(t))
	rec := httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodGet, "/_hub/new-contexts", ""))
	if rec.Code != http.StatusOK {
		t.Fatalf("GET = %d %s", rec.Code, rec.Body)
	}
	var got struct {
		UnderWay []newContextRun `json:"under_way"`
		Count    int             `json:"count"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil || got.Count != 0 || got.UnderWay == nil {
		t.Fatalf("body %s, err %v", rec.Body, err)
	}
	rec = httptest.NewRecorder()
	p.ServeHTTP(rec, loopbackReq(http.MethodPost, "/_hub/new-contexts", ""))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("POST = %d", rec.Code)
	}
}
