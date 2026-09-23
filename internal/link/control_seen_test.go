package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// The orchestrator's question, answered: has the human seen my last turn, and
// which of its questions are still owed. See docs/seen-design.md.

func seenBoard(t *testing.T) *httptest.Server {
	t.Helper()
	orch := map[string]any{
		"id": "c1", "wire_name": "orch", "status": "needs-input",
		"seen": map[string]any{
			"turn_ended_at": "2026-09-23T12:00:00.000Z", "unseen": true,
			"open_questions": []string{"land sa21 first?", "build the tray?"},
			"answered":       false,
		},
	}
	worker := map[string]any{"id": "c2", "wire_name": "sa25", "status": "running"}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []any{orch, worker}})
		case strings.HasPrefix(r.URL.Path, "/v1/tasks/c1"):
			_ = json.NewEncoder(w).Encode(orch)
		case strings.HasPrefix(r.URL.Path, "/v1/tasks/c2"):
			_ = json.NewEncoder(w).Encode(worker)
		default:
			http.NotFound(w, r)
		}
	}))
}

// An empty card is the caller's own, which is how the orchestrator asks about
// itself.
func TestTaskWithNoCardIsYourOwnAndCarriesSeen(t *testing.T) {
	srv := seenBoard(t)
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.taskHandler(context.Background(), ctlReq("orch", ""), taskInput{})
	if err != nil {
		t.Fatal(err)
	}
	if out.Card != "c1" || out.Seen == nil || !out.Seen.Unseen || len(out.Seen.OpenQuestions) != 2 {
		t.Fatalf("got %+v seen=%+v", out, out.Seen)
	}

	// A card with no turn yet has no seen block at all.
	_, out, err = c.taskHandler(context.Background(), ctlReq("orch", ""), taskInput{Card: "sa25"})
	if err != nil || out.Seen != nil {
		t.Fatalf("a card with no turn: %+v %v", out.Seen, err)
	}

	// No card and no identity is a sentence, not a crash.
	if _, _, err := c.taskHandler(context.Background(), ctlReq("", ""), taskInput{}); err == nil {
		t.Fatal("no card and no caller did not refuse")
	}
}

func TestPeersCountUnseenAndOpenQuestions(t *testing.T) {
	srv := seenBoard(t)
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.peersHandler(context.Background(), ctlReq("sa25", ""), peersInput{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Peers) != 1 || !out.Peers[0].Unseen || out.Peers[0].OpenQuestions != 2 {
		t.Fatalf("got %+v", out.Peers)
	}
}

func TestOpenCountNeverReportsAnUnreadableBlockAsNone(t *testing.T) {
	no := false
	s := &ctlSeen{QuestionsUnparsed: true, Answered: &no}
	if s.openCount() != 1 {
		t.Fatalf("an unreadable block counted %d", s.openCount())
	}
	var none *ctlSeen
	if none.openCount() != 0 {
		t.Fatal("no seen block counted questions")
	}
}
