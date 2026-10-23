//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type asked struct {
	path string
	body map[string]any
}

func fakeRoom(t *testing.T, status int, reply string) *[]asked {
	t.Helper()
	var got []asked
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		a := asked{path: r.URL.Path}
		_ = json.Unmarshal(raw, &a.body)
		got = append(got, a)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	loc := filepath.Join(t.TempDir(), "daemon.json")
	raw, _ := json.Marshal(map[string]any{"board": srv.URL, "agent": srv.URL, "pid": os.Getpid()})
	if err := os.WriteFile(loc, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ATRIUM_LOCATION", loc)
	t.Setenv("ATRIUM_SHARED_LOCATION", "-")
	t.Setenv("ATRIUM_HUB_URL", "")
	return &got
}

func fakeHubBoard(t *testing.T, status int, reply string) *int {
	t.Helper()
	n := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n++
		w.WriteHeader(status)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	old := backlogHubAddr
	backlogHubAddr = func() string { return strings.TrimPrefix(srv.URL, "http://") }
	t.Cleanup(func() { backlogHubAddr = old })
	return &n
}

func noHubBoard(t *testing.T) {
	t.Helper()
	old := backlogHubAddr
	backlogHubAddr = func() string { return "127.0.0.1:1" }
	t.Cleanup(func() { backlogHubAddr = old })
}

func runHuman(t *testing.T, args ...string) (string, error) {
	t.Helper()
	root := newRoot()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs(args)
	err := root.Execute()
	return buf.String(), err
}

func TestBacklogStatusFromAnotherRoomGoesThroughItsLink(t *testing.T) {
	t.Setenv("ATRIUM_AGENT_NAME", "")
	noHubBoard(t)
	got := fakeRoom(t, 200, `{"id":"o-x","status":"done"}`)
	out, err := runHuman(t, "backlog", "status", "o-x", "done")
	if err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].path != "/v1/hub/ask" {
		t.Fatalf("asked %+v", *got)
	}
	b := (*got)[0].body
	inner, _ := b["body"].(map[string]any)
	if b["method"] != "POST" || b["path"] != "/_hub/backlog/o-x" || inner["status"] != "done" {
		t.Fatalf("asked the room %v", b)
	}
	if !strings.Contains(out, "o-x is now done") {
		t.Fatalf("out %q", out)
	}
}

func TestBacklogFileRefusedByTheHubsBoardIsAskedThroughTheRoom(t *testing.T) {
	t.Setenv("ATRIUM_AGENT_NAME", "dir-ops")
	hub := fakeHubBoard(t, 403, `{"error":"written only from the machine the hub runs on"}`)
	got := fakeRoom(t, 200, `{"id":"o-7","status":"open","title":"t"}`)
	out, err := runHuman(t, "backlog", "file", "--dept", "ops", "--title", "t", "--priority", "high")
	if err != nil {
		t.Fatal(err)
	}
	if *hub != 1 || len(*got) != 1 || (*got)[0].body["from"] != "dir-ops" || (*got)[0].body["path"] != "/_hub/backlog" {
		t.Fatalf("hub %d, room %+v", *hub, *got)
	}
	if !strings.Contains(out, "filed o-7") {
		t.Fatalf("out %q", out)
	}
}

func TestBacklogPriorityAndListFromAnotherRoom(t *testing.T) {
	noHubBoard(t)
	got := fakeRoom(t, 200, `{"id":"o-x","priority":"low"}`)
	out, err := runHuman(t, "backlog", "priority", "o-x", "low")
	if err != nil || !strings.Contains(out, "o-x priority is now low") {
		t.Fatalf("%q %v", out, err)
	}
	inner, _ := (*got)[0].body["body"].(map[string]any)
	if inner["priority"] != "low" {
		t.Fatalf("asked %v", (*got)[0].body)
	}
	got = fakeRoom(t, 200, `{"items":[{"id":"o-y","dept":"ops","title":"y","status":"open"}]}`)
	out, err = runHuman(t, "backlog", "list", "--dept", "ops")
	if err != nil || !strings.Contains(out, "o-y") || (*got)[0].body["method"] != "GET" ||
		(*got)[0].body["path"] != "/_hub/backlog?dept=ops&open=1" {
		t.Fatalf("%q %v %+v", out, err, *got)
	}
}

func TestBacklogNamedBoardIsNeverAskedAgainThroughTheRoom(t *testing.T) {
	got := fakeRoom(t, 200, `{}`)
	_, _, _, _, err := runBacklog(t, 403, `{"error":"written only from the hub's machine"}`, "", "backlog", "status", "a", "done")
	if err == nil || err.Error() != "written only from the hub's machine" || len(*got) != 0 {
		t.Fatalf("err %v, room asked %+v", err, *got)
	}
}

func TestBacklogSaysBothWhenNeitherTheHubNorTheRoomAnswers(t *testing.T) {
	noHubBoard(t)
	fakeRoom(t, 503, `{"error":"this atrium is not a room linked to a hub"}`)
	_, err := runHuman(t, "backlog", "status", "a", "done")
	if err == nil || !strings.Contains(err.Error(), "the hub is not answering") ||
		!strings.Contains(err.Error(), "not a room linked to a hub") {
		t.Fatalf("err %v", err)
	}
}

func TestTellSendsKind(t *testing.T) {
	got := fakeRoom(t, 200, `{"queued":true}`)
	if _, err := runHuman(t, "tell", "sa2", "hello", "--name", "sa1", "--kind", "FYI"); err != nil {
		t.Fatal(err)
	}
	if len(*got) != 1 || (*got)[0].path != "/tell" || (*got)[0].body["kind"] != "fyi" {
		t.Fatalf("asked %+v", *got)
	}
	if _, err := runHuman(t, "tell", "sa2", "hello", "--name", "sa1"); err != nil {
		t.Fatal(err)
	}
	if _, ok := (*got)[1].body["kind"]; ok {
		t.Fatalf("a tell without --kind sent one: %v", (*got)[1].body)
	}
	if _, err := runHuman(t, "tell", "sa2", "hello", "--name", "sa1", "--kind", "urgent"); err == nil ||
		!strings.Contains(err.Error(), "fyi or needs") || len(*got) != 2 {
		t.Fatalf("err %v, asked %d", err, len(*got))
	}
}

func TestCIRunsAsksTheRoomsHubDoor(t *testing.T) {
	got := fakeRoom(t, 200, `{"kind":"github","runs":[{"id":42,"workflow":"ci","status":"completed",`+
		`"conclusion":"failure","head_sha":"abcdef1234567","branch":"main"}]}`)
	out, err := runHuman(t, "ci", "runs", "dovholuknf/atrium", "--branch", "main", "--limit", "3")
	if err != nil {
		t.Fatal(err)
	}
	b := (*got)[0].body
	if (*got)[0].path != "/v1/hub/ci" || b["action"] != "runs" || b["host"] != "github.com" || b["org"] != "dovholuknf" ||
		b["repo"] != "atrium" || b["branch"] != "main" || b["limit"] != float64(3) {
		t.Fatalf("asked %+v", *got)
	}
	if !strings.Contains(out, "42") || !strings.Contains(out, "failure") || !strings.Contains(out, "abcdef1234 ") {
		t.Fatalf("out %q", out)
	}
	if _, err := runHuman(t, "ci", "runs", "atrium"); err == nil || len(*got) != 1 {
		t.Fatalf("a bad repo was asked: %v", err)
	}
}

func TestCIRunPrintsJobsAndSteps(t *testing.T) {
	got := fakeRoom(t, 200, `{"kind":"github","run":{"id":42,"workflow":"ci","status":"completed","conclusion":"failure",`+
		`"jobs":[{"id":7,"name":"test","status":"completed","conclusion":"failure","steps":[{"number":3,"name":"go test",`+
		`"status":"completed","conclusion":"failure"}]}]}}`)
	out, err := runHuman(t, "ci", "run", "dovholuknf/atrium", "42")
	if err != nil {
		t.Fatal(err)
	}
	if (*got)[0].body["action"] != "run" || (*got)[0].body["run_id"] != float64(42) {
		t.Fatalf("asked %+v", *got)
	}
	if !strings.Contains(out, "job 7  test") || !strings.Contains(out, "go test") {
		t.Fatalf("out %q", out)
	}
}

func TestCILogWritesTheLogToAFileAndPrintsItsPath(t *testing.T) {
	got := fakeRoom(t, 200, `{"kind":"github","log":{"run_id":42,"failed_only":true,"lines":2,"truncated":true,`+
		`"text":"one\ntwo\n"}}`)
	dest := filepath.Join(t.TempDir(), "ci.log")
	out, err := runHuman(t, "ci", "log", "dovholuknf/atrium", "--run", "42", "--tail", "50", "--failed-only=false",
		"--out", dest)
	if err != nil {
		t.Fatal(err)
	}
	b := (*got)[0].body
	if b["action"] != "log" || b["run_id"] != float64(42) || b["tail"] != float64(50) || b["failed_only"] != false {
		t.Fatalf("asked %v", b)
	}
	raw, err := os.ReadFile(dest)
	if err != nil || string(raw) != "one\ntwo\n" {
		t.Fatalf("file %q %v", raw, err)
	}
	if !strings.Contains(out, filepath.ToSlash(dest)) || !strings.Contains(out, "2 lines") ||
		!strings.Contains(out, "longer log") {
		t.Fatalf("out %q", out)
	}
	if _, err := runHuman(t, "ci", "log", "dovholuknf/atrium"); err == nil || len(*got) != 1 {
		t.Fatalf("a log with no run or job was asked: %v", err)
	}
}

func TestCILogDefaultFileIsUnderTheTempFolder(t *testing.T) {
	t.Setenv("TMP", t.TempDir())
	t.Setenv("TMPDIR", os.Getenv("TMP"))
	fakeRoom(t, 200, `{"kind":"github","log":{"job_id":7,"lines":1,"text":"x"}}`)
	out, err := runHuman(t, "ci", "log", "dovholuknf/atrium", "--job", "7")
	if err != nil {
		t.Fatal(err)
	}
	path := strings.TrimSpace(strings.SplitN(out, "\n", 2)[0])
	if !strings.HasSuffix(path, "atrium-ci/dovholuknf-atrium-job-7.log") {
		t.Fatalf("path %q", path)
	}
	if raw, err := os.ReadFile(path); err != nil || string(raw) != "x" {
		t.Fatalf("file %q %v", raw, err)
	}
}

func TestCIArtifactsAndArtifact(t *testing.T) {
	got := fakeRoom(t, 200, `{"kind":"github","artifacts":[{"id":1,"name":"ci-logs","size_bytes":2048}]}`)
	out, err := runHuman(t, "ci", "artifacts", "dovholuknf/atrium", "42")
	if err != nil || (*got)[0].body["action"] != "artifacts" || !strings.Contains(out, "ci-logs") {
		t.Fatalf("%q %v %+v", out, err, *got)
	}
	got = fakeRoom(t, 200, `{"kind":"github","download":{"name":"ci-logs","dir":"C:/hub/ci/42","size_bytes":9,`+
		`"files":[{"path":"go-test.txt","size_bytes":9}],"note":"kept a day"}}`)
	out, err = runHuman(t, "ci", "artifact", "dovholuknf/atrium", "42", "ci-logs")
	if err != nil || (*got)[0].body["name"] != "ci-logs" || !strings.Contains(out, "C:/hub/ci/42") ||
		!strings.Contains(out, "go-test.txt") {
		t.Fatalf("%q %v %+v", out, err, *got)
	}
	got = fakeRoom(t, 200, `{"kind":"github","download":{"name":"ci-logs","file":"go-test.txt","text":"FAIL"}}`)
	dest := filepath.Join(t.TempDir(), "f.txt")
	out, err = runHuman(t, "ci", "artifact", "dovholuknf/atrium", "42", "ci-logs", "--file", "go-test.txt", "--out", dest)
	if err != nil || (*got)[0].body["file"] != "go-test.txt" || !strings.Contains(out, filepath.ToSlash(dest)) {
		t.Fatalf("%q %v %+v", out, err, *got)
	}
	if raw, _ := os.ReadFile(dest); string(raw) != "FAIL" {
		t.Fatalf("file %q", raw)
	}
}

func TestCIRefusalIsTheHubsSentence(t *testing.T) {
	fakeRoom(t, 502, `{"error":"not supported on bitbucket"}`)
	_, err := runHuman(t, "ci", "runs", "bitbucket.org/a/b")
	if err == nil || !strings.Contains(err.Error(), "not supported on bitbucket") {
		t.Fatalf("err %v", err)
	}
}
