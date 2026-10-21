//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/dovholuknf/atrium/internal/daemon"
)

func TestTheReviewTagIsTheOneTheRoomReads(t *testing.T) {
	if ReviewTag != daemon.ReviewTag {
		t.Fatalf("hub says %q, room reads %q", ReviewTag, daemon.ReviewTag)
	}
}

func TestAReviewLaunchWritesTheRulesAndTheBaseAheadOfTheCallersBrief(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tasks" {
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{}})
			return
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "rv"})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	_, _, err := c.launchHandler(context.Background(), ctlReq("a", "beta"),
		launchInput{Item: "f-test", Cwd: "/work/dir", Kind: "review", Base: "main", Brief: "look at the tests too"})
	if err != nil {
		t.Fatal(err)
	}
	brief, _ := got["brief"].(string)
	rules, base, extra := strings.Index(brief, "# How to run this review"), strings.Index(brief, "Base branch: main"), strings.Index(brief, "look at the tests too")
	if rules != 0 || base < rules || extra < base || !strings.Contains(brief, "atrium_review") {
		t.Errorf("rules at %d, base at %d, brief at %d:\n%s", rules, base, extra, brief)
	}
	tags, _ := got["tags"].([]any)
	var names []string
	for _, x := range tags {
		names = append(names, x.(string))
	}
	if !slices.Contains(names, ReviewTag) {
		t.Errorf("tags = %v, want %s", names, ReviewTag)
	}
	if b, ok := got["base"]; ok && b != "" {
		t.Errorf("base was sent to the room as where a branch starts: %v", b)
	}
	if p, _ := got["prompt"].(string); !strings.Contains(p, "atrium_done") {
		t.Errorf("a review card is a worker and still ends with a report: %q", p)
	}
}

func TestOnlyTheReviewClassIsServedAtriumReview(t *testing.T) {
	for _, c := range []ctlClass{classFull, classWorker, classFullResident, classWorkerResident, classWorkerItem} {
		if inClass("atrium_review", c) {
			t.Errorf("class %d is served atrium_review", c)
		}
	}
	if !inClass("atrium_review", classWorkerReview) || !inClass("atrium_done", classWorkerReview) || inClass("atrium_launch", classWorkerReview) {
		t.Error("the review class is the worker set and atrium_review")
	}
}

func reviewBoard(t *testing.T, posted *[]map[string]any) *classHarness {
	t.Helper()
	tasks := []map[string]any{
		{"id": "rv1", "wire_name": "reviewer", "status": "running", "created_at": "2026-01-01T00:00:00Z", "tags": []string{ReviewTag, SubagentTag}},
		{"id": "wk1", "wire_name": "worker", "status": "running", "created_at": "2026-01-01T00:00:00Z", "tags": []string{SubagentTag}},
	}
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": tasks})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/rv1/atrium-review":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			*posted = append(*posted, body)
			_ = json.NewEncoder(w).Encode(map[string]any{"num": 3, "lines_vs_base": 12})
		case r.URL.Path == "/v1/tasks/wk1/atrium-review":
			w.WriteHeader(http.StatusForbidden)
			_ = json.NewEncoder(w).Encode(map[string]any{"error": "only a card launched as a review holds one"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(board.Close)
	c := &controlMCP{board: board.URL, client: board.Client()}
	ts := httptest.NewServer(c.handler())
	t.Cleanup(ts.Close)
	return &classHarness{mcpURL: ts.URL}
}

func TestAReviewCardIsShownAtriumReviewAndNobodyElseIs(t *testing.T) {
	var posted []map[string]any
	h := reviewBoard(t, &posted)
	if names := toolNames(t, h.connect(t, "reviewer", "beta")); !slices.Contains(names, "atrium_review") || !slices.Contains(names, "atrium_done") || slices.Contains(names, "atrium_launch") {
		t.Errorf("review card tools: %v", names)
	}
	if names := toolNames(t, h.connect(t, "worker", "beta")); slices.Contains(names, "atrium_review") {
		t.Errorf("a plain worker is served atrium_review: %v", names)
	}
	if names := toolNames(t, h.connect(t, "", "")); slices.Contains(names, "atrium_review") {
		t.Errorf("the full set is served atrium_review: %v", names)
	}
}

func TestAtriumReviewForwardsTheCallToTheCallersOwnCard(t *testing.T) {
	var posted []map[string]any
	h := reviewBoard(t, &posted)
	sess := h.connect(t, "reviewer", "beta")
	res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_review", Arguments: map[string]any{
		"action": "finding", "cat": "DEAD", "title": "unused", "site": "a.go:3",
		"edits": []map[string]any{{"path": "a.go", "old": "x", "new": "y"}},
	}})
	if err != nil || res.IsError {
		t.Fatalf("%v %+v", err, res)
	}
	if len(posted) != 1 || posted[0]["action"] != "finding" || posted[0]["cat"] != "DEAD" {
		t.Fatalf("posted %+v", posted)
	}
	edits, _ := posted[0]["edits"].([]any)
	if len(edits) != 1 || edits[0].(map[string]any)["old"] != "x" {
		t.Errorf("edits: %+v", posted[0]["edits"])
	}
	raw, _ := json.Marshal(res.StructuredContent)
	if !strings.Contains(string(raw), `"num":3`) || !strings.Contains(string(raw), `"lines_vs_base":12`) {
		t.Errorf("answer: %s", raw)
	}

	if res, err := sess.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_review", Arguments: map[string]any{"action": "dance"}}); err == nil && !res.IsError {
		t.Error("an unknown action was forwarded")
	}
}
