//go:build integration

package link

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/gitsync"
	"github.com/dovholuknf/atrium/internal/hubstore"
)

func TestFlowRolesStagesChecksAndDeploys(t *testing.T) {
	x := newBacklogRig(t)
	if code, body := x.send("POST", "/_hub/backlog", js(map[string]any{"id": "f-x", "dept": "fabric", "title": "do x"}), loopAddr); code != http.StatusCreated {
		t.Fatalf("file: %d %s", code, body)
	}
	code, body := x.send("POST", "/_hub/flow/roles", js(map[string]any{"do": "set", "role": "orchestrator", "room": "sg4", "card": "c1", "by": "clint"}), loopAddr)
	if code != 200 || !strings.Contains(body, `"c1"`) {
		t.Fatalf("role set: %d %s", code, body)
	}
	code, body = x.send("GET", "/_hub/flow/roles?role=orchestrator", "", "203.0.113.9:4000")
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
	code, _ := x.send("POST", "/_hub/flow/roles", js(map[string]any{"do": "set", "role": "orchestrator", "room": "sg4", "card": "c1"}), "203.0.113.9:4000")
	if code != http.StatusForbidden {
		t.Fatalf("got %d", code)
	}
}

type fakeFlowGit struct {
	facts   gitsync.ItemFacts
	covered map[string]bool
}

func (f *fakeFlowGit) ItemFacts(context.Context, string, string) (gitsync.ItemFacts, error) {
	return f.facts, nil
}

func (f *fakeFlowGit) EpochPlan(context.Context) (gitsync.EpochPlan, error) {
	return gitsync.EpochPlan{OldTip: "aaaaaaa", Base: "bbbbbbb", Replay: []string{"ccccccc"}}, nil
}

func (f *fakeFlowGit) EpochMove(_ context.Context, _ int64, branch, expectOld string) (string, string, error) {
	if branch == "" || expectOld != "aaaaaaa" {
		return "", "", hubstore.ErrNoEpoch
	}
	return "aaaaaaa", "ddddddd", nil
}

func (f *fakeFlowGit) CoveredBy(_ context.Context, base, _ string) (bool, error) {
	return f.covered[base], nil
}

// THE STAGE IS COMPUTED WHEN AN ITEM IS READ: git raises it, the checks and the deploys raise it further, and nobody
// reported any of it.
func TestFlowStageIsReadFromGitChecksAndDeploys(t *testing.T) {
	x := newBacklogRig(t)
	g := &fakeFlowGit{covered: map[string]bool{}}
	x.p.SetFlowGit(g)
	x.send("POST", "/_hub/backlog", js(map[string]any{"id": "f-x", "dept": "fabric", "title": "do x"}), loopAddr)
	x.send("POST", "/_hub/flow/items/f-x", js(map[string]any{"stage": "implementing", "branch": "claude/x"}), loopAddr)
	stage := func() (string, map[string]any) {
		code, body := x.send("GET", "/_hub/flow/items/f-x", "", loopAddr)
		if code != 200 {
			t.Fatalf("get: %d %s", code, body)
		}
		o := obj(t, body)
		return str(o, "stage"), o
	}
	if s, _ := stage(); s != "implementing" {
		t.Fatalf("no facts: %s", s)
	}
	g.facts = gitsync.ItemFacts{SHA: "abc1234", Implemented: true}
	if s, _ := stage(); s != "implemented" {
		t.Fatalf("on the hub: %s", s)
	}
	g.facts.Merged = true
	if s, _ := stage(); s != "merged" {
		t.Fatalf("merged: %s", s)
	}
	// A passing check on a sha that does not cover the item changes nothing; one that does makes it tested.
	x.send("POST", "/_hub/flow/checks", js(map[string]any{"sha": "def5678", "name": "sanity", "result": "pass"}), loopAddr)
	if s, _ := stage(); s != "merged" {
		t.Fatalf("a check that does not cover it: %s", s)
	}
	g.covered["def5678"] = true
	if s, _ := stage(); s != "tested" {
		t.Fatalf("tested: %s", s)
	}
	// Two rooms report deploys: one runs it, one does not. Deployed waits for both.
	x.send("POST", "/_hub/flow/deploys", js(map[string]any{"room": "sg4", "sha": "def5678"}), loopAddr)
	x.send("POST", "/_hub/flow/deploys", js(map[string]any{"room": "sg3", "sha": "0000000"}), loopAddr)
	s, o := stage()
	if s != "tested" || !strings.Contains(js(o["behind_rooms"]), "sg3") || !strings.Contains(js(o["deployed_on"]), "sg4") {
		t.Fatalf("one room behind: %s %v", s, o)
	}
	g.covered["0000000"] = true
	if s, _ = stage(); s != "deployed" {
		t.Fatalf("deployed: %s", s)
	}
	g.facts.PushedByClint = true
	if s, _ = stage(); s != "pushed-by-clint" {
		t.Fatalf("pushed: %s", s)
	}
	// Git going quiet never lowers a stage.
	g.facts = gitsync.ItemFacts{}
	if s, _ = stage(); s != "pushed-by-clint" {
		t.Fatalf("lowered: %s", s)
	}
}

func TestFlowEpochOpensAndClosesThroughTheGitPlan(t *testing.T) {
	x := newBacklogRig(t)
	x.p.SetFlowGit(&fakeFlowGit{covered: map[string]bool{}})
	code, body := x.send("POST", "/_hub/flow/epoch", js(map[string]any{"do": "open", "by": "clint"}), loopAddr)
	if code != 200 || str(obj(t, body), "fetched_sha") != "aaaaaaa" {
		t.Fatalf("open takes claude/main's tip: %d %s", code, body)
	}
	if code, _ = x.send("POST", "/_hub/flow/epoch", js(map[string]any{"do": "open"}), loopAddr); code != http.StatusBadRequest {
		t.Fatalf("second open: %d", code)
	}
	code, body = x.send("GET", "/_hub/flow/epoch", "", "203.0.113.9:4000")
	if code != 200 || !strings.Contains(body, `"ccccccc"`) || !strings.Contains(body, `"open"`) {
		t.Fatalf("read: %d %s", code, body)
	}
	if code, _ = x.send("POST", "/_hub/flow/epoch", js(map[string]any{"do": "close", "expect": "zzzzzzz", "branch": "next"}), loopAddr); code == 200 {
		t.Fatal("a stale close was taken")
	}
	code, body = x.send("POST", "/_hub/flow/epoch", js(map[string]any{"do": "close", "expect": "aaaaaaa", "branch": "next", "by": "merger"}), loopAddr)
	if code != 200 || str(obj(t, body), "new_tip") != "ddddddd" || str(obj(t, body), "state") != "closed" {
		t.Fatalf("close: %d %s", code, body)
	}
	if code, _ = x.send("POST", "/_hub/flow/epoch", js(map[string]any{"do": "close"}), loopAddr); code != http.StatusBadRequest && code != http.StatusNotFound {
		t.Fatalf("close with none open: %d", code)
	}
}
