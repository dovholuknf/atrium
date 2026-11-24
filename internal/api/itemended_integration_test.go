//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func endedCard(t *testing.T, st *store.Store, tags ...string) *store.Task {
	t.Helper()
	c := cardIn(t, st, t.TempDir())
	if err := st.SetTags(c.ID, tags); err != nil {
		t.Fatal(err)
	}
	return c
}

func itemEnded(t *testing.T, h interface {
	ServeHTTP(http.ResponseWriter, *http.Request)
}, card, status string) map[string]any {
	t.Helper()
	rec := post(t, h, "/v1/tasks/"+card+"/item-ended", map[string]any{"item": "f-x", "status": status, "by": "orch"})
	if rec.Code != 200 {
		t.Fatalf("%d %s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &out)
	return out
}

func TestAnEndedItemClosesAnAgentLaunchedCardAndLeavesTheRest(t *testing.T) {
	srv, st, h := serverFor(t)
	srv.StopRunnerBy = func(string, string) error { return nil }
	agent := []string{"origin:agent", "atrium:item:f-x"}
	for name, tags := range map[string][]string{
		"human":        {"atrium:item:f-x"},
		"resident":     append([]string{"atrium:resident"}, agent...),
		"orchestrator": append([]string{"atrium:orchestrator"}, agent...),
		"director":     append([]string{"atrium:director"}, agent...),
		"other item":   {"origin:agent", "atrium:item:f-y"},
	} {
		c := endedCard(t, st, tags...)
		if out := itemEnded(t, h, c.ID, "done"); out["action"] != "kept" {
			t.Fatalf("%s: %v", name, out)
		}
		if got, _ := st.Get(c.ID); got.Status == store.StatusDone {
			t.Fatalf("%s was closed", name)
		}
	}
	c := endedCard(t, st, agent...)
	if out := itemEnded(t, h, c.ID, "dropped"); out["closed"] != true {
		t.Fatalf("agent card: %v", out)
	}
	if got, _ := st.Get(c.ID); got.Status != store.StatusDone {
		t.Fatalf("status = %s", got.Status)
	}
}

func TestAnItemHeldExitsTheRunnerAndKeepsTheCard(t *testing.T) {
	srv, st, h := serverFor(t)
	var stopped string
	srv.StopRunnerBy = func(id, _ string) error { stopped = id; return nil }
	c := endedCard(t, st, "origin:agent", "atrium:item:f-x")
	if err := st.SetStatus(c.ID, store.StatusRunning); err != nil {
		t.Fatal(err)
	}
	if out := itemEnded(t, h, c.ID, "held"); out["action"] != "exited" || stopped != c.ID {
		t.Fatalf("out = %v stopped = %q", out, stopped)
	}
	if got, _ := st.Get(c.ID); got.Status == store.StatusDone {
		t.Fatal("a held item closed its card")
	}
}
