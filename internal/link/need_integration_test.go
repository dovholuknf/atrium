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

func TestTheElevatedTagIsTheOneTheRoomWrites(t *testing.T) {
	if ElevatedTag != daemon.ElevatedTag {
		t.Fatalf("hub reads %q, room writes %q", ElevatedTag, daemon.ElevatedTag)
	}
}

func TestAnElevatedWorkerGetsTheLaunchToolsAndKeepsItsOwnSet(t *testing.T) {
	h := newClassHarness(t, []map[string]any{
		{"id": "w1", "wire_name": "worker1", "status": "working", "tags": []string{"atrium:subagent", ElevatedTag}},
		{"id": "i1", "wire_name": "itemw", "status": "working", "tags": []string{"atrium:subagent", ItemTag, ElevatedTag}},
		{"id": "r1", "wire_name": "rev", "status": "working", "tags": []string{"atrium:subagent", ReviewTag, ElevatedTag}},
		{"id": "d1", "wire_name": "boss", "status": "working", "tags": []string{"atrium:director", ElevatedTag}},
	})
	want := append(append([]string{}, workerSeven...), "atrium_launch", "atrium_launch_worker")
	slices.Sort(want)
	sameNames(t, "elevated worker", toolNames(t, h.connect(t, "worker1", "alpha")), want)
	for _, who := range []string{"itemw", "rev"} {
		got := toolNames(t, h.connect(t, who, "alpha"))
		if !slices.Contains(got, "atrium_launch") || !slices.Contains(got, "atrium_launch_worker") {
			t.Fatalf("%s tools %v, want the launch tools", who, got)
		}
	}
	if got := toolNames(t, h.connect(t, "itemw", "alpha")); slices.Contains(got, "atrium_say") {
		t.Fatalf("an elevated item worker got atrium_say: %v", got)
	}
	if got := toolNames(t, h.connect(t, "rev", "alpha")); !slices.Contains(got, "atrium_review") {
		t.Fatalf("an elevated review worker lost atrium_review: %v", got)
	}
	sameNames(t, "elevated director", toolNames(t, h.connect(t, "boss", "alpha")), fullFifteen)
}

func needBoard(t *testing.T, posted *map[string]any) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "w1", "wire_name": "worker1", "status": "working", "tags": []string{"atrium:subagent"}}}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/w1/need":
			_ = json.NewDecoder(r.Body).Decode(posted)
			_ = json.NewEncoder(w).Encode(map[string]any{"card": "w1", "restarting": true,
				"granted": []string{"tool:atrium_launch"}, "title": "review panel"})
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestAGrantedNeedTellsTheHumanWhatAndWhyAndDropsTheCachedClass(t *testing.T) {
	var posted map[string]any
	board := needBoard(t, &posted)
	fg := &fakeGrowls{}
	g := NewGrowler(fg)
	c := &controlMCP{board: board.URL, client: board.Client(), growler: func() *Growler { return g }}
	c.classes = map[string]classEntry{"alpha\x00worker1": {}}
	ts := httptest.NewServer(c.handler())
	t.Cleanup(ts.Close)
	h := &classHarness{c: c, mcpURL: ts.URL}
	s := h.connect(t, "worker1", "alpha")

	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_need",
		Arguments: map[string]any{"needs": []string{"tool:atrium_launch"}, "why": "the panel launches its reviewers"}})
	if err != nil || res.IsError {
		t.Fatalf("need: %v %+v", err, res)
	}
	if posted["why"] != "the panel launches its reviewers" {
		t.Fatalf("the room was sent %v", posted)
	}
	if len(fg.raised) != 1 {
		t.Fatalf("raised %+v, want one", fg.raised)
	}
	row := fg.raised[0]
	if row.Room != "alpha" || row.Reason != ReasonQuestion || !strings.Contains(row.Title, "review panel") ||
		!strings.Contains(row.Title, "tool:atrium_launch") || !strings.Contains(row.Body, "the panel launches its reviewers") {
		t.Fatalf("growler %+v", row)
	}
	c.classMu.Lock()
	_, cached := c.classes["alpha\x00worker1"]
	c.classMu.Unlock()
	if cached {
		t.Fatal("the caller's class is still cached after a grant")
	}
}

func TestANeedWithNoWhyIsRefusedBeforeTheRoomIsAsked(t *testing.T) {
	var posted map[string]any
	board := needBoard(t, &posted)
	fg := &fakeGrowls{}
	g := NewGrowler(fg)
	c := &controlMCP{board: board.URL, client: board.Client(), growler: func() *Growler { return g }}
	ts := httptest.NewServer(c.handler())
	t.Cleanup(ts.Close)
	h := &classHarness{c: c, mcpURL: ts.URL}
	s := h.connect(t, "worker1", "alpha")
	res, err := s.CallTool(context.Background(), &mcp.CallToolParams{Name: "atrium_need",
		Arguments: map[string]any{"needs": []string{"tool:Skill"}, "why": ""}})
	if err == nil && !res.IsError {
		t.Fatal("a need with no why was taken")
	}
	if posted != nil || len(fg.raised) != 0 {
		t.Fatalf("posted %v, raised %+v", posted, fg.raised)
	}
}
