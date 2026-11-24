//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestAnItemGoingDoneDroppedOrHeldTellsItsCardsRoom(t *testing.T) {
	for _, status := range []string{"done", "dropped", "held"} {
		x := recordRig(t, "claude/f-x")
		x.calls = nil
		fileItem(t, x)
		if code, body := x.send("POST", "/_hub/backlog/f-x", js(map[string]any{"status": status, "by": "orch@room-e"}), loopAddr); code != http.StatusOK {
			t.Fatalf("%s: %d %s", status, code, body)
		}
		x.p.WaitWork(10 * time.Second)
		want := "room-e /v1/tasks/c1/item-ended"
		if len(x.calls) != 1 || x.calls[0] != want {
			t.Fatalf("%s: calls = %v", status, x.calls)
		}
	}
}

func TestAnItemGoingInProgressOrOpenTellsNoRoom(t *testing.T) {
	x := recordRig(t, "claude/f-x")
	x.calls = nil
	fileItem(t, x)
	for _, status := range []string{"in-progress", "open"} {
		if code, body := x.send("POST", "/_hub/backlog/f-x", js(map[string]any{"status": status, "by": "orch@room-e"}), loopAddr); code != http.StatusOK {
			t.Fatalf("%s: %d %s", status, code, body)
		}
	}
	x.p.WaitWork(10 * time.Second)
	if len(x.calls) != 0 {
		t.Fatalf("calls = %s", strings.Join(x.calls, ";"))
	}
}

func fileItem(t *testing.T, x *itemRig) {
	t.Helper()
	if code, body := x.send("POST", "/_hub/backlog", js(map[string]any{"id": "f-x", "dept": "fabric", "title": "x",
		"by": "orch@room-e"}), loopAddr); code != http.StatusCreated && code != http.StatusConflict {
		t.Fatalf("file: %d %s", code, body)
	}
}
