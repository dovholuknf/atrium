//go:build integration

package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/seenhost"
)

func TestDoneRefusesALinkOnTheWorkersMachine(t *testing.T) {
	for _, bad := range []string{"mock at http://127.0.0.1:7931/", "see localhost:8080", "FILE:///tmp/x.html"} {
		for name, in := range map[string]doneInput{
			"line": {SHA: "abc1234", Line: bad}, "why": {SHA: "abc1234", Why: bad},
			"unverified": {SHA: "abc1234", Unverified: bad}, "artifact": {Artifact: bad},
		} {
			d, got := fakeEnd()
			_, _, err := d.Done(context.Background(), nil, in)
			if err == nil || !strings.Contains(err.Error(), "atrium_link") || len(*got) != 0 {
				t.Fatalf("%s %q: err = %v, recorded %d, want the rule and nothing recorded", name, bad, err, len(*got))
			}
		}
	}
}

func TestBlockedRefusesALinkOnTheWorkersMachine(t *testing.T) {
	d, got := fakeEnd()
	_, _, err := d.Blocked(context.Background(), nil, blockedInput{Reason: "open http://127.0.0.1:7931/ to see it"})
	if err == nil || !strings.Contains(err.Error(), "atrium_link") || len(*got) != 0 {
		t.Fatalf("err = %v, recorded %d, want the rule and nothing recorded", err, len(*got))
	}
}

func TestSayFromAWorkerRefusesALinkOnTheWorkersMachine(t *testing.T) {
	sent := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/v1/tasks":
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "w1", "wire_name": "worker", "status": "running", "tags": []string{SubagentTag}},
				{"id": "d1", "wire_name": "boss", "status": "running"},
			}})
		case strings.HasSuffix(r.URL.Path, "/message"):
			sent = true
			_ = json.NewEncoder(w).Encode(map[string]any{"delivered": "terminal"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	_, _, err := c.sayHandler(context.Background(), ctlReq("worker", "r1"), sayInput{To: "boss", Text: "see http://127.0.0.1:7931/"})
	if err == nil || !strings.Contains(err.Error(), "atrium_link") || sent {
		t.Fatalf("worker say: err = %v, sent = %v, want the rule and nothing sent", err, sent)
	}
	if _, _, err := c.sayHandler(context.Background(), ctlReq("boss", "r1"), sayInput{To: "worker", Text: "try localhost:3000"}); err != nil || !sent {
		t.Fatalf("a director's say was refused: err = %v, sent = %v", err, sent)
	}
}

func TestLinkAnswersTheBoardLinkForAFileInTheCard(t *testing.T) {
	seenhost.Reset()
	t.Cleanup(seenhost.Reset)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
			{"id": "w1", "wire_name": "worker", "status": "running"},
		}})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}
	_, out, err := c.linkHandler(context.Background(), ctlReq("worker", "room-d"), linkInput{Path: "./.uncommitted/preview/a b.html"})
	if err != nil {
		t.Fatal(err)
	}
	if want := srv.URL + "/room/room-d/w1/files/.uncommitted/preview/a%20b.html"; out.URL != want {
		t.Fatalf("url = %q, want %q", out.URL, want)
	}
	for _, bad := range []string{"", "../x", "/etc/passwd", "a/../../x"} {
		if _, _, err := c.linkHandler(context.Background(), ctlReq("worker", "room-d"), linkInput{Path: bad}); err == nil {
			t.Fatalf("path %q was not refused", bad)
		}
	}
	if _, _, err := c.linkHandler(context.Background(), ctlReq("", ""), linkInput{Path: "x.html"}); err == nil {
		t.Fatal("a call with no card was not refused")
	}
}
