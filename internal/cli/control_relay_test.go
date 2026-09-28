package cli

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// The stdio atrium control against a fake room board. It must name its sender,
// take `name@room`, and still work against a room older than /v1/say. See
// docs/cross-room-say-design.md.

type stdioBoard struct {
	mu      sync.Mutex
	noSay   bool
	said    []map[string]string
	message []map[string]string
	report  map[string]string
}

func (b *stdioBoard) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	b.mu.Lock()
	defer b.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	switch {
	case r.URL.Path == "/v1/say" && !b.noSay:
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.said = append(b.said, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "held", "to": body["to"]})
	case r.URL.Path == "/v1/tasks":
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "c1", "wire_name": "sa1", "status": "working"},
			{"id": "c2", "wire_name": "sa2", "status": "working"},
		}})
	case r.URL.Path == "/v1/tasks/c2/message":
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		b.message = append(b.message, body)
		_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "queued", "when": "immediate"})
	case r.URL.Path == "/v1/tasks/c1/report":
		_ = json.NewDecoder(r.Body).Decode(&b.report)
		_ = json.NewEncoder(w).Encode(map[string]any{"recorded": true, "status": "done", "launcher_told": true})
	default:
		w.Header().Set("Content-Type", "text/plain")
		http.NotFound(w, r)
	}
}

func stdioAgainst(t *testing.T, b *stdioBoard, room string) {
	t.Helper()
	srv := httptest.NewServer(b)
	t.Cleanup(srv.Close)
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	t.Setenv("ATRIUM_AGENT_NAME", "sa1")
	t.Setenv("ATRIUM_ROOM", room)
}

// RULE 3 AND RULE 2. The stdio say names its sender, so it is never typed as
// the operator, and passes `name@room` to the room to relay.
func TestStdioSayNamesTheSenderAndTakesAnotherRoom(t *testing.T) {
	b := &stdioBoard{}
	stdioAgainst(t, b, "m1mini")

	_, out, err := sayHandler(context.Background(), nil, SayInput{To: "atrium-87300@claude-sg4", Text: "hi"})
	if err != nil {
		t.Fatalf("say: %v", err)
	}
	if len(b.said) != 1 || b.said[0]["from"] != "sa1" || b.said[0]["to"] != "atrium-87300@claude-sg4" {
		t.Fatalf("the room got %+v", b.said)
	}
	if out.Delivered != "held" || !strings.Contains(out.Note, "held") {
		t.Fatalf("out = %+v", out)
	}
}

// A room older than /v1/say still takes a bare name, now with the sender, and
// refuses another room with a sentence.
func TestStdioSayAgainstAnOlderRoom(t *testing.T) {
	b := &stdioBoard{noSay: true}
	stdioAgainst(t, b, "m1mini")

	_, out, err := sayHandler(context.Background(), nil, SayInput{To: "sa2", Text: "hi"})
	if err != nil || out.Card != "c2" {
		t.Fatalf("say = %+v, %v", out, err)
	}
	if len(b.message) != 1 || b.message[0]["from"] != "sa1" {
		t.Fatalf("the message went as %+v, want it from sa1", b.message)
	}
	_, _, err = sayHandler(context.Background(), nil, SayInput{To: "x@claude-sg4", Text: "hi"})
	if err == nil || !strings.Contains(err.Error(), "older") {
		t.Fatalf("err = %v, want the room called older", err)
	}
}

// atrium_report on the stdio server files on the caller's own card.
func TestStdioReportFilesOnTheCallersCard(t *testing.T) {
	b := &stdioBoard{}
	stdioAgainst(t, b, "m1mini")

	_, out, err := reportHandler(context.Background(), nil, ReportInput{Status: "done", Summary: "did it", SHA: "abc"})
	if err != nil || !out.Recorded || !out.LauncherTold {
		t.Fatalf("report = %+v, %v", out, err)
	}
	if b.report["status"] != "done" || b.report["recap"] != "did it" || b.report["sha"] != "abc" {
		t.Fatalf("the room got %+v", b.report)
	}
}
