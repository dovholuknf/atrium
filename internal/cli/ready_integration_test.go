//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// `atrium ready` prints the daemon's line when the ack lands, and fails with its
// reason when it is refused.
func TestReadyPrintsTheLineOrFailsWithTheReason(t *testing.T) {
	var got map[string]any
	refuse := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ready" {
			t.Errorf("posted to %s", r.URL.Path)
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		if refuse {
			w.WriteHeader(http.StatusConflict)
			_, _ = w.Write([]byte(`{"error":"write your handoff to /x.md first, then run atrium ready again"}`))
			return
		}
		_, _ = w.Write([]byte(`{"ok":true,"stored":true,"path":"/x.md","message":"End your turn now."}`))
	}))
	defer srv.Close()
	t.Setenv("ATRIUM_TASK_ID", "t-1")

	var out bytes.Buffer
	if err := reportReady(&out, srv.URL, "worker"); err != nil {
		t.Fatal(err)
	}
	if got["agent"] != "worker" || got["task_id"] != "t-1" {
		t.Fatalf("sent %v", got)
	}
	if !strings.Contains(out.String(), "End your turn now.") {
		t.Fatalf("printed %q", out.String())
	}

	refuse = true
	err := reportReady(&out, srv.URL, "worker")
	if err == nil || !strings.Contains(err.Error(), "write your handoff to /x.md first") {
		t.Fatalf("a refusal did not fail with its reason: %v", err)
	}
}
