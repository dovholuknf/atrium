//go:build integration

package link

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/hubstore"
	"github.com/dovholuknf/atrium/internal/link/ctl"
)

const recSHA = "cccccccccccccccccccccccccccccccccccccccc"

// recordRig is an item rig whose room answers a stamp with a branch and a sha, and has a builder card on f-x.
func recordRig(t *testing.T, branch string) *itemRig {
	t.Helper()
	x := newItemRig(t)
	x.p.itemRoomCall = func(_ context.Context, room, path string, body []byte) (int, []byte, error) {
		x.calls = append(x.calls, room+" "+path)
		return http.StatusOK, []byte(`{"at_sha":"` + recSHA + `","dirty":true,"branch":"` + branch + `","log_path":"/x/log"}`), nil
	}
	code, body := x.send("POST", "/_hub/items/sync", js(map[string]any{"item_id": "it-b", "room": "sg4", "card": "c1",
		"generation": 1, "state": "running", "revision": 1, "owner_kind": "human", "created_by": "orch",
		"work_id": "f-x", "step": "building"}), loopAddr)
	if code != http.StatusOK {
		t.Fatalf("attach: %d %s", code, body)
	}
	return x
}

func (x *itemRig) record(extra map[string]any) (int, string) {
	b := map[string]any{"room": "sg4", "card": "c1", "kind": "test", "cmd": "go test ./...", "result": "pass"}
	for k, v := range extra {
		b[k] = v
	}
	return x.send("POST", "/_hub/work/record", js(b), loopAddr)
}

func TestARecordIsReportedAtTheShaTheRoomReadAndDirty(t *testing.T) {
	x := recordRig(t, "claude/f-x")
	code, body := x.record(nil)
	if code != http.StatusOK {
		t.Fatalf("record: %d %s", code, body)
	}
	o := obj(t, body)
	if o["at_sha"] != recSHA || o["dirty"] != true || o["tag"] != "reported" || o["ready"] != false {
		t.Fatalf("answer = %s", body)
	}
	if !strings.Contains(str(o, "tested"), "plus uncommitted changes") {
		t.Fatalf("tested = %v", o["tested"])
	}
	if w, _ := x.st.WorkByID("f-x"); w.Branch != "claude/f-x" {
		t.Fatalf("the first record should adopt the branch: %q", w.Branch)
	}
}

func TestARecordFromAnotherBranchIsRefused(t *testing.T) {
	x := recordRig(t, "claude/other")
	if err := x.st.SetWorkBranch("f-x", "claude/f-x"); err != nil {
		t.Fatal(err)
	}
	code, body := x.record(nil)
	if code != http.StatusConflict || !strings.Contains(body, "claude/other") || !strings.Contains(body, "claude/f-x") {
		t.Fatalf("other branch: %d %s", code, body)
	}
	if fs, _ := x.st.WorkFacts("f-x"); len(fs) != 0 {
		t.Fatalf("a refused record left a fact: %+v", fs)
	}
}

func TestARecordFromMainIsRefused(t *testing.T) {
	x := recordRig(t, "claude/main")
	if code, body := x.record(nil); code != http.StatusConflict {
		t.Fatalf("main: %d %s", code, body)
	}
}

func TestACardCannotNameAShaOrRecordAReview(t *testing.T) {
	x := recordRig(t, "claude/f-x")
	if code, body := x.record(map[string]any{"sha": recSHA}); code != http.StatusBadRequest || !strings.Contains(body, "never names a commit") {
		t.Fatalf("sha: %d %s", code, body)
	}
	if code, body := x.record(map[string]any{"at_sha": recSHA}); code != http.StatusBadRequest {
		t.Fatalf("at_sha: %d %s", code, body)
	}
	if code, body := x.record(map[string]any{"kind": "review"}); code != http.StatusForbidden {
		t.Fatalf("review: %d %s", code, body)
	}
	if fs, _ := x.st.WorkFacts("f-x"); len(fs) != 0 {
		t.Fatalf("a refused record left a fact: %+v", fs)
	}
}

func TestTheReviewRouteRefusesACardAndAnotherOrigin(t *testing.T) {
	x := recordRig(t, "claude/f-x")
	body := js(map[string]any{"verdict": "ok"})
	req := func(addr, agent string) int {
		r, _ := http.NewRequest("POST", "/_hub/work/f-x/review", strings.NewReader(body))
		r.RemoteAddr, r.Host = addr, "127.0.0.1"
		r.Header.Set("Content-Type", "application/json")
		if agent != "" {
			r.Header.Set(ctl.AgentHeader, agent)
		}
		rec := httptest.NewRecorder()
		x.p.ServeHTTP(rec, r)
		return rec.Code
	}
	if c := req(loopAddr, "sg4/c1"); c != http.StatusForbidden {
		t.Fatalf("a card's review: %d", c)
	}
	if c := req("203.0.113.9:4000", ""); c != http.StatusForbidden {
		t.Fatalf("a review from elsewhere: %d", c)
	}
	if code, b := x.send("POST", "/_hub/work/f-x/review", js(map[string]any{"verdict": "ok", "sha": recSHA}), loopAddr); code != http.StatusBadRequest {
		t.Fatalf("a review naming a sha: %d %s", code, b)
	}
	if fs, _ := x.st.WorkFacts("f-x"); len(fs) != 0 {
		t.Fatalf("a refused review left a fact: %+v", fs)
	}
	_ = hubstore.FactReview
}
