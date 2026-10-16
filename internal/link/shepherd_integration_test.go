//go:build integration

package link

import (
	"net/http"
	"strings"
	"testing"
)

func TestAShepherdRowIsWrittenOnlyFromTheHubsMachineAndReadFromAnywhere(t *testing.T) {
	x := newItemRig(t)
	if code, _ := x.send("GET", "/_hub/shepherds/bug-x", "", loopAddr); code != http.StatusNotFound {
		t.Fatalf("before a row: %d", code)
	}
	row := js(map[string]any{"item_id": "other", "phase": "wait", "card": "c1"})
	if code, _ := x.send("POST", "/_hub/shepherds/bug-x", row, "203.0.113.9:4000"); code != http.StatusForbidden {
		t.Fatalf("a write from elsewhere: %d", code)
	}
	code, body := x.send("POST", "/_hub/shepherds/bug-x", row, loopAddr)
	// THE PATH NAMES THE ROW, so a body cannot write another's.
	if code != http.StatusOK || str(obj(t, body), "item_id") != "bug-x" {
		t.Fatalf("write: %d %s", code, body)
	}
	code, body = x.send("GET", "/_hub/shepherds/bug-x", "", "203.0.113.9:4000")
	if code != http.StatusOK || !strings.Contains(body, `"phase":"wait"`) {
		t.Fatalf("read: %d %s", code, body)
	}
	if code, _ = x.send("POST", "/_hub/shepherds/bug-x", js(map[string]any{"phase": ""}), loopAddr); code != http.StatusBadRequest {
		t.Fatalf("a row with no phase: %d", code)
	}
}
