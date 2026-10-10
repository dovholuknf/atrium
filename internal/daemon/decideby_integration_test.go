//go:build integration

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/store"
)

// r-020: the hub's board-wide auto switch approves through the room's decide
// route, and the room records it as `global-auto`, not as the operator.
func TestTheHubsAutoApprovalIsRecordedAsGlobalAuto(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "decide-by-parked")

	answer := postPermission(context.Background(), d, `{"agent":"decide-by-parked","command":"go test ./..."}`)
	id := parkedID(t, d, task.ID)

	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	resp, err := http.Post(ts.URL+"/v1/permissions/"+id+"/decide", "application/json",
		strings.NewReader(`{"decision":"approve","reason":"hub global auto","by":"global-auto"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("decide answered %d", resp.StatusCode)
	}
	select {
	case got := <-answer:
		if got.Decision != "approve" {
			t.Fatalf("the hook was answered %+v", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("the decision did not wake the parked hook")
	}
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != DecidedByHubAuto {
		t.Fatalf("recorded as decided by %q, want %q", p.DecidedBy, DecidedByHubAuto)
	}
}

// The fallback path, where nobody is parked any more, records the same decider.
func TestAGlobalAutoDecisionWithNoHookParkedIsRecordedTheSame(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "decide-by-gone")

	ctx, hangUp := context.WithCancel(context.Background())
	answer := postPermission(ctx, d, `{"agent":"decide-by-gone","command":"sleep 100"}`)
	id := parkedID(t, d, task.ID)
	hangUp()
	<-answer
	deadline := time.Now().Add(3 * time.Second)
	for d.liveStoreIDs()[id] {
		if time.Now().After(deadline) {
			t.Fatal("the hung-up request is still live")
		}
		time.Sleep(10 * time.Millisecond)
	}

	p, err := d.decideBy(id, "approve", "hub global auto", "", DecidedByHubAuto)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != DecidedByHubAuto {
		t.Fatalf("recorded as decided by %q", p.DecidedBy)
	}
}

// No caller can name any other decider, and leaving `by` out is the operator.
func TestTheDecideRouteRefusesAForgedDecider(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	task := permTask(t, d, "decide-by-forged")

	answer := postPermission(context.Background(), d, `{"agent":"decide-by-forged","command":"rm -rf /"}`)
	id := parkedID(t, d, task.ID)

	ts := httptest.NewServer(d.ap.Handler())
	defer ts.Close()
	for _, forged := range []string{"auto", "you", "shelved", "git push"} {
		resp, err := http.Post(ts.URL+"/v1/permissions/"+id+"/decide", "application/json",
			strings.NewReader(`{"decision":"approve","by":"`+forged+`"}`))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("by %q answered %d, want 400", forged, resp.StatusCode)
		}
	}
	if p, _ := d.st.GetPermission(id); p == nil || p.DecidedAt != nil {
		t.Fatal("a refused decider still decided the request")
	}

	// And with no `by` at all, it is the operator, as it always was.
	resp, err := http.Post(ts.URL+"/v1/permissions/"+id+"/decide", "application/json",
		strings.NewReader(`{"decision":"block","reason":"no"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	<-answer
	p, err := d.st.GetPermission(id)
	if err != nil {
		t.Fatal(err)
	}
	if p.DecidedBy != store.DecidedBySelf {
		t.Fatalf("a decision with no by was recorded as %q", p.DecidedBy)
	}
}
