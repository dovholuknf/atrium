package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// runBacklog runs the root command against a fake hub that records what it was asked and answers `reply`.
func runBacklog(t *testing.T, status int, reply string, stdin string, args ...string) (out, method, path, body string, err error) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		method, path, body = r.Method, r.URL.RequestURI(), string(raw)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	defer srv.Close()
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetIn(strings.NewReader(stdin))
	root.SetArgs(append(args, "--board-addr", strings.TrimPrefix(srv.URL, "http://")))
	err = root.Execute()
	return buf.String(), method, path, body, err
}

func TestBacklogListAsksForOpenItemsAndPrintsThem(t *testing.T) {
	out, method, path, _, err := runBacklog(t, 200,
		`{"items":[{"id":"f-new-x","dept":"fabric","title":"do x","status":"open"}]}`, "", "backlog", "list", "--dept", "fabric")
	if err != nil {
		t.Fatal(err)
	}
	if method != "GET" || path != "/_hub/backlog?dept=fabric&open=1" {
		t.Fatalf("asked %s %s", method, path)
	}
	if !strings.Contains(out, "f-new-x") || !strings.Contains(out, "do x") {
		t.Fatalf("out %q", out)
	}
}

func TestBacklogListAllAsksForNoOpenFilter(t *testing.T) {
	_, _, path, _, err := runBacklog(t, 200, `{"items":[]}`, "", "backlog", "list", "--all")
	if err != nil || path != "/_hub/backlog?" {
		t.Fatalf("path %q err %v", path, err)
	}
}

func TestBacklogFileSendsTheItemAndReadsBodyFromStdin(t *testing.T) {
	t.Setenv("ATRIUM_AGENT_NAME", "dir-fabric")
	t.Setenv("ATRIUM_ROOM", "m1mini")
	out, method, path, body, err := runBacklog(t, 201, `{"id":"f-new-y","status":"open","title":"why"}`, "the words",
		"backlog", "file", "f-new-y", "--dept", "fabric", "--title", "why", "--body", "-")
	if err != nil {
		t.Fatal(err)
	}
	var got map[string]string
	if err := json.Unmarshal([]byte(body), &got); err != nil {
		t.Fatal(err)
	}
	if method != "POST" || path != "/_hub/backlog" || got["id"] != "f-new-y" || got["body"] != "the words" ||
		got["by"] != "dir-fabric" || got["room"] != "m1mini" {
		t.Fatalf("sent %s %s %v", method, path, got)
	}
	if !strings.Contains(out, "filed f-new-y") {
		t.Fatalf("out %q", out)
	}
}

func TestBacklogStatusAndAHubRefusalIsShownAsTheHubSaidIt(t *testing.T) {
	_, method, path, body, err := runBacklog(t, 200, `{"id":"a","status":"done"}`, "", "backlog", "status", "a", "done")
	if err != nil || method != "POST" || path != "/_hub/backlog/a" || !strings.Contains(body, `"status":"done"`) {
		t.Fatalf("%s %s %s %v", method, path, body, err)
	}
	_, _, _, _, err = runBacklog(t, 403, `{"error":"written only from the hub's machine"}`, "", "backlog", "status", "a", "done")
	if err == nil || err.Error() != "written only from the hub's machine" {
		t.Fatalf("err %v", err)
	}
}

func TestReportsAddListAndRead(t *testing.T) {
	_, method, path, body, err := runBacklog(t, 201, `{"id":"r1"}`, "", "reports", "add", "--to", "fabric", "--subject", "hi", "--body", "b")
	if err != nil || method != "POST" || path != "/_hub/reports" || !strings.Contains(body, `"subject":"hi"`) {
		t.Fatalf("%s %s %s %v", method, path, body, err)
	}
	out, _, path, _, err := runBacklog(t, 200, `{"reports":[{"id":"r1","to_dept":"fabric","subject":"hi","read_at":null}]}`, "",
		"reports", "list", "--unread")
	if err != nil || path != "/_hub/reports?unread=1" || !strings.Contains(out, "unread") {
		t.Fatalf("%q %q %v", out, path, err)
	}
	_, method, path, body, err = runBacklog(t, 200, `{"id":"r1","subject":"hi","body":"b"}`, "", "reports", "read", "r1")
	if err != nil || method != "POST" || path != "/_hub/reports/r1" || !strings.Contains(body, `"do":"read"`) {
		t.Fatalf("%s %s %s %v", method, path, body, err)
	}
	_, method, _, _, err = runBacklog(t, 200, `{"id":"r1","subject":"hi"}`, "", "reports", "read", "r1", "--peek")
	if err != nil || method != "GET" {
		t.Fatalf("peek used %s %v", method, err)
	}
}
