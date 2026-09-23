package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon"
)

// The hub's half of agent-to-agent reliability. See
// docs/a2a-reliability-design.md.

// The room recognises an agent launch by the same string the hub stamps.
func TestTheOriginTagIsTheOneTheRoomReads(t *testing.T) {
	if OriginTag != daemon.OriginAgentTag {
		t.Fatalf("hub stamps %q, room reads %q", OriginTag, daemon.OriginAgentTag)
	}
}

// a2aBoard stands in for the hub's own board.
type a2aBoard struct {
	launch map[string]any
	report map[string]any
	reply  map[string]any
}

func (b *a2aBoard) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "w1", "wire_name": "worker", "status": "running"},
				{"id": "g1", "wire_name": "gem", "status": "running"},
			}})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			_ = json.NewDecoder(r.Body).Decode(&b.launch)
			_ = json.NewEncoder(w).Encode(map[string]any{"id": "new", "wire_name": "kid"})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/w1/report":
			_ = json.NewDecoder(r.Body).Decode(&b.report)
			_ = json.NewEncoder(w).Encode(b.reply)
		case r.Method == http.MethodPost && r.URL.Path == "/v1/tasks/g1/message":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"delivered": "undeliverable", "reachable": "no",
				"warning": "queued, but gem has no way to receive it",
			})
		default:
			http.NotFound(w, r)
		}
	})
}

// The launch carries who asked for it, so the room can route reports back,
// and the prompt ends with the one line of the contract a step list leaves out.
func TestLaunchSaysWhoLaunchedItAndAsksForAReport(t *testing.T) {
	board := &a2aBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	if _, _, err := c.launchHandler(context.Background(), ctlReq("orchestrator", "beta"),
		launchInput{Cwd: "/work/dir", Prompt: "do one, two, three"}); err != nil {
		t.Fatal(err)
	}
	if board.launch["spawned_by"] != "orchestrator" {
		t.Fatalf("spawned_by = %v, want the caller", board.launch["spawned_by"])
	}
	prompt, _ := board.launch["prompt"].(string)
	if !strings.HasPrefix(prompt, "do one, two, three") || !strings.HasSuffix(prompt, reportLine) {
		t.Fatalf("prompt = %q, want the caller's then the report line", prompt)
	}
}

// A launch with no prompt gets none invented.
func TestLaunchWithNoPromptGetsNoReportLine(t *testing.T) {
	board := &a2aBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	if _, _, err := c.launchHandler(context.Background(), ctlReq("orchestrator", "beta"),
		launchInput{Cwd: "/work/dir"}); err != nil {
		t.Fatal(err)
	}
	if board.launch["prompt"] != "" {
		t.Fatalf("prompt = %q, want empty", board.launch["prompt"])
	}
}

// F15: the room's warning reaches the sender at send time.
func TestSayPassesTheUndeliverableWarningThrough(t *testing.T) {
	board := &a2aBoard{}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.sayHandler(context.Background(), ctlReq("worker", "beta"),
		sayInput{To: "gem", Text: "hello"})
	if err != nil {
		t.Fatal(err)
	}
	if out.Delivered != "undeliverable" || !strings.Contains(out.Note, "no way to receive") {
		t.Fatalf("say answered %+v, want the room's warning", out)
	}
}

// The report lands on the caller's own card, found from its identity header.
func TestReportGoesToTheCallersOwnCard(t *testing.T) {
	board := &a2aBoard{reply: map[string]any{
		"recorded": true, "status": "done", "unverified": true, "launcher_told": true,
	}}
	srv := httptest.NewServer(board.handler())
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.reportHandler(context.Background(), ctlReq("worker", "beta"),
		reportInput{Status: "done", Summary: "it works", SHA: "abc1234"})
	if err != nil {
		t.Fatal(err)
	}
	if board.report["sha"] != "abc1234" || board.report["recap"] != "it works" || board.report["status"] != "done" {
		t.Fatalf("the room got %v", board.report)
	}
	if !out.Recorded || !out.LauncherTold || !out.Unverified || !strings.Contains(out.Note, "not in your worktree") {
		t.Fatalf("report answered %+v", out)
	}
}

// A caller atrium cannot name has no card to report on.
func TestReportWithNoCallerIsRefused(t *testing.T) {
	c := &controlMCP{board: "http://127.0.0.1:1", client: http.DefaultClient}
	if _, _, err := c.reportHandler(context.Background(), ctlReq("", "beta"),
		reportInput{Status: "done"}); err == nil {
		t.Fatal("a report from nobody was accepted")
	}
}
