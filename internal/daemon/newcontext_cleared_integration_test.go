//go:build integration

package daemon

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
)

// clearedCycle runs an automatic cycle to its limit prompt, then sends the board's DELETE.
func clearedCycle(t *testing.T, d *Daemon) (id, file string, f *fakePTY) {
	t.Helper()
	task, f, _ := cycleCard(t, d, 300_000)
	d.cycleCheck(task, 300_000)
	until(t, "the limit prompt", func() bool { c := d.Nctx.Get(task.ID); return prompts(f) == 1 && c != nil && c.Prompted == 1 })
	file = cycleFile(d, task.ID)
	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+task.ID+"/new-context", nil)
	req.SetPathValue("id", task.ID)
	rec := httptest.NewRecorder()
	d.HandleNewContext(rec, req)
	if rec.Code != http.StatusOK || d.Nctx.Get(task.ID) != nil {
		t.Fatalf("the DELETE answered %d and left %v", rec.Code, d.Nctx.Get(task.ID))
	}
	return task.ID, file, f
}

func TestAClearedCycleOverTheLimitIsNotStartedAgain(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	id, _, f := clearedCycle(t, d)
	task, _ := d.st.Get(id)
	d.cycleCheck(task, 300_000)
	d.cycleCheck(task, 305_000)
	if d.Nctx.Get(id) != nil || prompts(f) != 1 {
		t.Fatalf("a second cycle started: %d prompts, run %v", prompts(f), d.Nctx.Get(id))
	}
	d.cycleCheck(task, 100_000)
	d.cycleCheck(task, 300_000)
	until(t, "a cycle again once the card was under its limit", func() bool { return d.Nctx.Get(id) != nil })
}

func TestAReadyAfterTheBoardClearedTheCycleIsTheAck(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	id, file, f := clearedCycle(t, d)
	task, _ := d.st.Get(id)
	if err := os.WriteFile(file, handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	if _, code, err := d.Ready(task); err != nil {
		t.Fatalf("refused with %d: %v", code, err)
	}
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if prompts(f) != 1 {
		t.Fatalf("%d limit prompts, want 1", prompts(f))
	}
}
