package link

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// atrium_wake_after_restart queues on the CALLER's card and nobody else's, and
// clears with DELETE. The room does the rest. See docs/runtime/restart-wake.md.
func TestWakeAfterRestartQueuesOnTheCallersOwnCard(t *testing.T) {
	var got struct {
		method, path, room, by string
		body                   map[string]any
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/tasks" {
			_ = json.NewEncoder(w).Encode(map[string]any{"tasks": []map[string]any{
				{"id": "o1", "wire_name": "orchestrator", "status": "running"},
				{"id": "w1", "wire_name": "worker", "status": "running"},
			}})
			return
		}
		got.method, got.path, got.room, got.by = r.Method, r.URL.Path, r.Header.Get(RoomHeader), r.URL.Query().Get("by")
		got.body = nil
		_ = json.NewDecoder(r.Body).Decode(&got.body)
		_ = json.NewEncoder(w).Encode(map[string]any{"queued": true, "cleared": true, "replaced": "older"})
	}))
	defer srv.Close()
	c := &controlMCP{board: srv.URL, client: srv.Client()}

	_, out, err := c.wakeHandler(context.Background(), ctlReq("orchestrator", "beta"), wakeInput{Text: "we up"})
	if err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodPost || got.path != "/v1/tasks/o1/restart-wake" || got.room != "beta" {
		t.Fatalf("queued with %s %s room %q", got.method, got.path, got.room)
	}
	if got.body["text"] != "we up" || got.body["by"] != "orchestrator" {
		t.Fatalf("queued body %v", got.body)
	}
	if !out.Queued || out.Card != "o1" || out.Replaced != "older" {
		t.Fatalf("answer %+v", out)
	}

	if _, out, err = c.wakeHandler(context.Background(), ctlReq("orchestrator", "beta"), wakeInput{Clear: true}); err != nil {
		t.Fatal(err)
	}
	if got.method != http.MethodDelete || got.path != "/v1/tasks/o1/restart-wake" || got.by != "orchestrator" || !out.Cleared {
		t.Fatalf("cleared with %s %s by %q: %+v", got.method, got.path, got.by, out)
	}

	if _, _, err := c.wakeHandler(context.Background(), ctlReq("", "beta"), wakeInput{Text: "x"}); err == nil {
		t.Fatal("a caller with no handle queued a wake")
	}
	if _, _, err := c.wakeHandler(context.Background(), ctlReq("orchestrator", "beta"), wakeInput{}); err == nil {
		t.Fatal("a wake with no text was queued")
	}
}
