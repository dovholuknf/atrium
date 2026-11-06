//go:build integration

package daemon

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
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

func TestAReadyBeforeTheLimitPromptIsTypedIsTheAckAndNoSecondPromptFollows(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	task, f, _ := cycleCard(t, d, 300_000)
	midTurn(d, task.ID)
	d.cycleCheck(task, 300_000)
	until(t, "the cycle", func() bool { return d.Nctx.Get(task.ID) != nil })
	ncAck(t, d, task)
	ncTurnEnds(d, task.ID)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	d.cycleCheck(task, 305_000)
	if prompts(f) > 1 {
		t.Fatalf("%d limit prompts, want at most 1", prompts(f))
	}
}

func TestAColdCycleClearedFromTheBoardIsNotStartedAgainOverTheLimit(t *testing.T) {
	d := testDaemon(t)
	task, f, _ := coldCard(t, d, store.KeepaliveOff)
	d.Ka.Tick(context.Background())
	until(t, "the limit prompt", func() bool { c := d.Nctx.Get(task.ID); return prompts(f) == 1 && c != nil && c.Prompted == 1 })
	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+task.ID+"/new-context", nil)
	req.SetPathValue("id", task.ID)
	d.HandleNewContext(httptest.NewRecorder(), req)
	task, _ = d.st.Get(task.ID)
	d.cycleCheck(task, 300_000)
	d.cycleCheck(task, 305_000)
	if d.Nctx.Get(task.ID) != nil || prompts(f) != 1 {
		t.Fatalf("a second cycle started: %d prompts, run %v", prompts(f), d.Nctx.Get(task.ID))
	}
}

func boardClear(d *Daemon, id string) int {
	req := httptest.NewRequest(http.MethodDelete, "/v1/tasks/"+id+"/new-context", nil)
	req.SetPathValue("id", id)
	rec := httptest.NewRecorder()
	d.HandleNewContext(rec, req)
	return rec.Code
}

// readyMidTurn is the card writing its handoff and running `atrium ready` inside a turn, after the board cleared it.
func readyMidTurn(t *testing.T, d *Daemon, id, file string) {
	t.Helper()
	midTurn(d, id)
	if err := os.WriteFile(file, handoffBody, 0o644); err != nil {
		t.Fatal(err)
	}
	task, _ := d.st.Get(id)
	if _, code, err := d.Ready(task); err != nil {
		t.Fatalf("refused with %d: %v", code, err)
	}
}

func TestABoardClearPastTheAckStillTypesTheClear(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	id, file, f := clearedCycle(t, d)
	readyMidTurn(t, d, id, file)
	code := boardClear(d, id)
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	if code != http.StatusConflict {
		t.Fatalf("a DELETE past the ack answered %d, want 409", code)
	}
}

func TestABoardClearAfterTheClearStillTypesTheWake(t *testing.T) {
	d := testDaemon(t)
	fastNewContext(t, d)
	id, file, f := clearedCycle(t, d)
	readyMidTurn(t, d, id, file)
	ncTurnEnds(d, id)
	until(t, "/clear", func() bool { return strings.Contains(f.written(), "/clear") })
	code := boardClear(d, id)
	d.WakeSawSession(id, "conv-new")
	until(t, "the wake", func() bool { return strings.Contains(f.written(), ncWakeMark) })
	if code != http.StatusConflict {
		t.Fatalf("a DELETE after /clear answered %d, want 409", code)
	}
}
