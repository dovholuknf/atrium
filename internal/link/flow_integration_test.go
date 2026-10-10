//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
)

func TestFlowRolesStagesChecksAndDeploys(t *testing.T) {
	x := newBacklogRig(t)
	if code, body := x.send("POST", "/_hub/backlog", js(map[string]any{"id": "f-x", "dept": "fabric", "title": "do x"}), loopAddr); code != http.StatusCreated {
		t.Fatalf("file: %d %s", code, body)
	}
	code, body := x.send("POST", "/_hub/flow/roles", js(map[string]any{"do": "set", "role": "merger", "room": "sg4", "card": "c1", "by": "clint"}), loopAddr)
	if code != 200 || !strings.Contains(body, `"c1"`) {
		t.Fatalf("role set: %d %s", code, body)
	}
	code, body = x.send("GET", "/_hub/flow/roles?role=merger", "", "203.0.113.9:4000")
	if code != 200 || !strings.Contains(body, `"c1"`) {
		t.Fatalf("roles: %d %s", code, body)
	}
	code, body = x.send("POST", "/_hub/flow/items/f-x", js(map[string]any{"stage": "implemented", "sha": "abc1234", "branch": "claude/x"}), loopAddr)
	if code != 200 || str(obj(t, body), "stage") != "implemented" {
		t.Fatalf("stage: %d %s", code, body)
	}
	x.sawEvent(t, "flow")
	if code, _ = x.send("POST", "/_hub/flow/items/f-x", js(map[string]any{"stage": "idea"}), loopAddr); code != http.StatusBadRequest {
		t.Fatalf("backwards: %d", code)
	}
	if code, _ = x.send("POST", "/_hub/flow/items/f-nope", js(map[string]any{"stage": "idea"}), loopAddr); code != http.StatusNotFound {
		t.Fatalf("no item: %d", code)
	}
	if code, body = x.send("POST", "/_hub/flow/checks", js(map[string]any{"sha": "abc1234", "name": "sanity", "result": "pass"}), loopAddr); code != 200 {
		t.Fatalf("check: %d %s", code, body)
	}
	if code, body = x.send("GET", "/_hub/flow/checks?sha=abc1234", "", loopAddr); code != 200 || !strings.Contains(body, "sanity") {
		t.Fatalf("checks: %d %s", code, body)
	}
	if code, body = x.send("POST", "/_hub/flow/deploys", js(map[string]any{"room": "sg4", "sha": "abc1234"}), loopAddr); code != 200 {
		t.Fatalf("deploy: %d %s", code, body)
	}
	if code, body = x.send("GET", "/_hub/flow/deploys", "", loopAddr); code != 200 || !strings.Contains(body, "abc1234") {
		t.Fatalf("deploys: %d %s", code, body)
	}
}

func TestFlowIsWrittenOnlyFromTheHubsMachine(t *testing.T) {
	x := newBacklogRig(t)
	code, _ := x.send("POST", "/_hub/flow/roles", js(map[string]any{"do": "set", "role": "merger", "room": "sg4", "card": "c1"}), "203.0.113.9:4000")
	if code != http.StatusForbidden {
		t.Fatalf("got %d", code)
	}
}
