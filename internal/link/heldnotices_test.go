package link

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// atrium_task's `notices` reads back what the room held on a card instead of
// typing it: `notified` events marked held, newest last, and nothing else.
func TestTaskNoticesReadsOnlyHeldNotices(t *testing.T) {
	events := `{"events":[
		{"at":"t1","kind":"prompted","payload":{"text":"go"}},
		{"at":"t2","kind":"notified","payload":{"by":"auto-context","done":true}},
		{"at":"t3","kind":"notified","payload":"not an object"},
		{"at":"t4","kind":"notified","payload":{"by":"atrium","held":true,"source":"silent-stop",
			"about":"worker","about_card":"w1","text":"worker ended its turn without reporting"}}`
	for i := 0; i < 25; i++ {
		events += fmt.Sprintf(`,{"at":"n%02d","kind":"notified",`+
			`"payload":{"held":true,"source":"context-size","text":"n%02d"}}`, i, i)
	}
	events += `]}`
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/tasks/o1":
			fmt.Fprint(w, `{"id":"o1","wire_name":"orch","status":"needs-input"}`)
		case "/v1/tasks/o1/events":
			fmt.Fprint(w, events)
		default:
			http.NotFound(w, r)
		}
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL, client: board.Client()}

	_, evs, notices, err := c.readCard(context.Background(), "", "o1", false, true)
	if err != nil {
		t.Fatal(err)
	}
	if evs != nil {
		t.Fatalf("events = %v, not asked for", evs)
	}
	if len(notices) != 20 || notices[0].Text != "n05" || notices[19].Text != "n24" {
		t.Fatalf("notices = %d, first %+v, want the last 20", len(notices), notices[0])
	}

	events = `{"events":[{"at":"t1","kind":"prompted","payload":{}},
		{"at":"t4","kind":"notified","payload":{"held":true,"source":"silent-stop","about":"worker",
		"about_card":"w1","text":"worker ended its turn without reporting"}}]}`
	_, evs, notices, err = c.readCard(context.Background(), "", "o1", true, true)
	if err != nil {
		t.Fatal(err)
	}
	want := heldNotice{At: "t4", Source: "silent-stop", About: "worker", Card: "w1",
		Text: "worker ended its turn without reporting"}
	if len(notices) != 1 || notices[0] != want || len(evs) != 2 {
		t.Fatalf("notices = %+v, events = %v", notices, evs)
	}
}

// A held fyi reads back saying it is an fyi and who sent it.
func TestTaskNoticesLabelsAnFYIWithItsSender(t *testing.T) {
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/tasks/o1":
			fmt.Fprint(w, `{"id":"o1","wire_name":"orch","status":"needs-input"}`)
		case "/v1/tasks/o1/events":
			fmt.Fprint(w, `{"events":[{"at":"t1","kind":"notified","payload":{"held":true,"source":"fyi",
				"about":"worker","about_card":"w1","text":"all quiet"}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL, client: board.Client()}

	_, _, notices, err := c.readCard(context.Background(), "", "o1", false, true)
	if err != nil {
		t.Fatal(err)
	}
	want := heldNotice{At: "t1", Source: "fyi", Kind: "fyi", About: "worker", Card: "w1",
		Text: "fyi from worker: all quiet"}
	if len(notices) != 1 || notices[0] != want {
		t.Fatalf("notices = %+v, want %+v", notices, want)
	}
}

// Reading your own notices posts the read marker, and reading another card's does not.
func TestTaskNoticesMarksOwnNoticesRead(t *testing.T) {
	var posted []string
	board := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.Method == http.MethodPost {
			posted = append(posted, r.URL.Path)
			fmt.Fprint(w, `{"ok":true}`)
			return
		}
		switch r.URL.Path {
		case "/v1/tasks":
			fmt.Fprint(w, `{"tasks":[{"id":"o1","wire_name":"orch","status":"needs-input"},
				{"id":"w1","wire_name":"worker","status":"running"}]}`)
		case "/v1/tasks/o1", "/v1/tasks/w1":
			fmt.Fprintf(w, `{"id":%q,"wire_name":"orch","status":"needs-input"}`, r.URL.Path[len("/v1/tasks/"):])
		case "/v1/tasks/o1/events", "/v1/tasks/w1/events":
			fmt.Fprint(w, `{"events":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer board.Close()
	c := &controlMCP{board: board.URL, client: board.Client()}
	req := &mcp.CallToolRequest{Extra: &mcp.RequestExtra{Header: http.Header{AgentHeader: {"orch"}}}}

	if _, _, err := c.taskHandler(context.Background(), req, taskInput{Notices: true}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 1 || posted[0] != "/v1/tasks/o1/notices-read" {
		t.Fatalf("own notices posted = %v, want the read marker", posted)
	}
	posted = nil
	if _, _, err := c.taskHandler(context.Background(), req, taskInput{Notices: true, Card: "w1"}); err != nil {
		t.Fatal(err)
	}
	if _, _, err := c.taskHandler(context.Background(), req, taskInput{}); err != nil {
		t.Fatal(err)
	}
	if len(posted) != 0 {
		t.Fatalf("another card's notices, or no notices, posted %v", posted)
	}
}
