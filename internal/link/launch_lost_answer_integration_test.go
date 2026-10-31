//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// lostAnswerBoard is a room whose launch starts the card and then loses the answer: the post is
// dropped, but a card in the directory is on the list afterwards. `made` is whether it starts one.
func lostAnswerBoard(made bool, drop func(http.ResponseWriter)) (*httptest.Server, *int) {
	posts := 0
	var cards []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": cards})
		case r.Method == http.MethodPost && r.URL.Path == "/v1/launch":
			posts++
			if made {
				cards = append(cards, map[string]any{"id": "c9", "wire_name": "kid", "display_title": "t",
					"worktree": `C:\Work\Dir\`, "created_at": time.Now().UTC().Format(time.RFC3339Nano)})
			}
			drop(w)
		default:
			http.NotFound(w, r)
		}
	}))
	return srv, &posts
}

// The answer is lost and the card exists: the launch says so, with the card, and does not fail, so
// nobody retries it into a second worker.
func TestALaunchWhoseAnswerIsLostReturnsTheCardItMade(t *testing.T) {
	srv, posts := lostAnswerBoard(true, func(w http.ResponseWriter) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"error":"context canceled"}`))
	})
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	out, err := c.LaunchOnRoom(context.Background(), launchInput{Item: "f-test", Cwd: "c:/work/dir"}, "claude", "", "", "", "", "")
	if err != nil {
		t.Fatalf("a launch that started was reported as failed: %v", err)
	}
	if out.Card != "c9" || !out.Recovered || !strings.Contains(out.Note, "do NOT launch it again") {
		t.Fatalf("out = %+v", out)
	}
	if *posts != 1 {
		t.Fatalf("posted %d times, want 1: finding the card must not launch again", *posts)
	}
}

// A relayed launch says the same across the hub: ok, with the card and a warning, not unconfirmed.
func TestARelayedLaunchWhoseAnswerIsLostIsAnswered(t *testing.T) {
	srv, _ := lostAnswerBoard(true, func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) })
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	out, err := c.LaunchOnRoom(context.Background(), launchInput{Item: "f-test", Cwd: "c:/work/dir"}, "claude", "", "", "", "", "")
	if err != nil || out.Card != "c9" {
		t.Fatalf("out = %+v, %v", out, err)
	}
	if w := strings.TrimSuffix(out.Note, LaunchStartedNote); !strings.Contains(w, "do NOT launch it again") {
		t.Fatalf("the relay's warning is %q", w)
	}
}

// No card in the directory: the failure stays a failure, as it always was.
func TestALaunchThatMadeNothingStaysFailed(t *testing.T) {
	srv, _ := lostAnswerBoard(false, func(w http.ResponseWriter) { w.WriteHeader(http.StatusBadGateway) })
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	if _, err := c.LaunchOnRoom(context.Background(), launchInput{Item: "f-test", Cwd: "c:/work/dir"}, "claude", "", "", "", "", ""); err == nil {
		t.Fatal("a launch that made no card succeeded")
	}
}

// A dropped connection is the case in the field, not a clean 502.
func TestALaunchWhoseConnectionDropsReturnsTheCardItMade(t *testing.T) {
	srv, _ := lostAnswerBoard(true, func(w http.ResponseWriter) {
		conn, _, err := w.(http.Hijacker).Hijack()
		if err == nil {
			_ = conn.Close()
		}
	})
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	out, err := c.LaunchOnRoom(context.Background(), launchInput{Item: "f-test", Cwd: "c:/work/dir"}, "claude", "", "", "", "", "")
	if err != nil || out.Card != "c9" || !out.Recovered {
		t.Fatalf("out = %+v, %v", out, err)
	}
}
