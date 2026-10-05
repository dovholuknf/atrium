package daemon

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dovholuknf/atrium/internal/forge"
)

// fakeExec replaces the two seams. Every exec is recorded, and nothing real runs.
type fakeExec struct {
	mu    sync.Mutex
	runs  [][]string
	slow  bool
	bytes int
}

func (f *fakeExec) install(t *testing.T) {
	oldLook, oldRun := preflightLook, preflightRun
	oldEach, oldTotal := preflightEachFor, preflightTotalFor
	t.Cleanup(func() {
		preflightLook, preflightRun = oldLook, oldRun
		preflightEachFor, preflightTotalFor = oldEach, oldTotal
	})
	preflightLook = func(name string) (string, error) {
		if strings.HasPrefix(name, "missing") {
			return "", io.EOF
		}
		return "/fake/bin/" + name, nil
	}
	preflightRun = func(ctx context.Context, exe string, args []string, out io.Writer) error {
		f.mu.Lock()
		f.runs = append(f.runs, append([]string{exe}, args...))
		f.mu.Unlock()
		if f.slow {
			<-ctx.Done()
			return ctx.Err()
		}
		if f.bytes > 0 {
			_, _ = out.Write(bytes.Repeat([]byte("x"), f.bytes))
			return nil
		}
		_, _ = io.WriteString(out, "fake 1.0\n")
		return nil
	}
}

func (f *fakeExec) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.runs)
}

func preflight(t *testing.T, d *Daemon, body string) (int, preflightAnswer) {
	t.Helper()
	req := httptest.NewRequest("POST", "/v1/preflight", strings.NewReader(body))
	rec := httptest.NewRecorder()
	d.BoardHandler().ServeHTTP(rec, req)
	var a preflightAnswer
	if rec.Code == http.StatusOK {
		if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
			t.Fatalf("answer is not JSON: %v: %s", err, rec.Body.String())
		}
	}
	return rec.Code, a
}

func TestPreflightKnownKeysRunTheirFixedCommand(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{}
	f.install(t)
	code, a := preflight(t, d, `{"tools":["go","pwsh"],"runner_auth":["claude","codex"]}`)
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	got := map[string]bool{}
	for _, r := range f.runs {
		got[strings.Join(r, " ")] = true
	}
	for _, want := range []string{
		"/fake/bin/go version", "/fake/bin/pwsh -v",
		"/fake/bin/claude auth status", "/fake/bin/codex login status",
	} {
		if !got[want] {
			t.Errorf("did not run %q, ran %v", want, f.runs)
		}
	}
	if it := a.Tools["go"]; !it.OK || it.Path != "/fake/bin/go" || it.Output != "fake 1.0" {
		t.Errorf("go answer: %+v", it)
	}
	if !a.RunnerAuth["claude"].OK {
		t.Errorf("claude answer: %+v", a.RunnerAuth["claude"])
	}
}

// The whole point of the verb: a body can never make the room run something.
func TestPreflightNeverExecutesAnUnknownKey(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{}
	f.install(t)
	code, a := preflight(t, d,
		`{"tools":["rm -rf /","curl","../evil"],"runner_auth":["evil","missing-runner"]}`)
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	if n := f.count(); n != 0 {
		t.Fatalf("an unknown key reached exec %d times: %v", n, f.runs)
	}
	it := a.Tools["curl"]
	if it.OK || it.Error != "unknown key, resolved at /fake/bin/curl" {
		t.Errorf("unknown key answer: %+v", it)
	}
	if e := a.RunnerAuth["missing-runner"].Error; e != "not found" {
		t.Errorf("an unknown key not on PATH answered %q", e)
	}
}

func TestPreflightEnvPresentIsABooleanNeverAValue(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{}
	f.install(t)
	const secret = "s3cr3t-value-do-not-leak"
	t.Setenv("ATRIUM_PREFLIGHT_TEST_SET", secret)
	req := httptest.NewRequest("POST", "/v1/preflight",
		strings.NewReader(`{"env_present":["ATRIUM_PREFLIGHT_TEST_SET","ATRIUM_PREFLIGHT_TEST_UNSET"]}`))
	rec := httptest.NewRecorder()
	d.BoardHandler().ServeHTTP(rec, req)
	if strings.Contains(rec.Body.String(), secret) {
		t.Fatalf("the answer carries a value: %s", rec.Body.String())
	}
	var a preflightAnswer
	if err := json.Unmarshal(rec.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	if !a.EnvPresent["ATRIUM_PREFLIGHT_TEST_SET"] || a.EnvPresent["ATRIUM_PREFLIGHT_TEST_UNSET"] {
		t.Errorf("env_present: %v", a.EnvPresent)
	}
}

func TestPreflightEachCommandIsBounded(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{slow: true}
	f.install(t)
	preflightEachFor = 50 * time.Millisecond
	_, a := preflight(t, d, `{"tools":["go"]}`)
	it := a.Tools["go"]
	if it.OK || it.Error != "timeout" {
		t.Fatalf("a slow command answered %+v", it)
	}
}

func TestPreflightTotalIsBounded(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{slow: true}
	f.install(t)
	preflightEachFor = 80 * time.Millisecond
	preflightTotalFor = 150 * time.Millisecond
	start := time.Now()
	_, a := preflight(t, d, `{"tools":["go","node","git","pwsh","npm","cmake"]}`)
	if el := time.Since(start); el > 2*time.Second {
		t.Fatalf("took %s against a 150ms total", el)
	}
	if n := f.count(); n >= 6 {
		t.Errorf("all %d commands ran past the total", n)
	}
	for k, it := range a.Tools {
		if it.Error != "timeout" {
			t.Errorf("%s answered %+v, want timeout", k, it)
		}
	}
	if len(a.Tools) != 6 {
		t.Errorf("got %d answers, want one per key", len(a.Tools))
	}
}

func TestPreflightOutputIsCapped(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{bytes: 1 << 20}
	f.install(t)
	_, a := preflight(t, d, `{"tools":["go"]}`)
	if n := len(a.Tools["go"].Output); n != preflightOutput {
		t.Fatalf("output is %d bytes, want the %d cap", n, preflightOutput)
	}
}

func TestPreflightReportsPidAndStartedBy(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{}
	f.install(t)
	if _, a := preflight(t, d, `{}`); a.StartedBy != "" || a.PID == 0 {
		t.Errorf("no --started-by should answer empty with a pid: %+v", a)
	}
	d.opts.StartedBy = "task-s4u:abc123"
	if _, a := preflight(t, d, `{}`); a.StartedBy != "task-s4u:abc123" {
		t.Errorf("started_by: %q", a.StartedBy)
	}
}

func TestPreflightIsOnTheHumanListenerOnly(t *testing.T) {
	d, _, cancel, _ := startDaemon(t)
	defer cancel()
	f := &fakeExec{}
	f.install(t)
	body := `{"tools":["go"]}`
	resp, err := http.Post("http://"+d.opts.HumanAddr+"/v1/preflight", "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("the human listener answered %d", resp.StatusCode)
	}
	before := f.count()
	for _, path := range []string{"/v1/preflight", "/preflight"} {
		resp, err := http.Post("http://"+d.opts.AgentAddr+path, "application/json", strings.NewReader(body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode == 200 {
			t.Errorf("the agent listener answered %s with 200", path)
		}
	}
	if f.count() != before {
		t.Error("the agent listener ran a command")
	}
}

// A ROOM WITH A HUB RUNS NO FORGE CLI in a preflight: the logins are the hub's, and the answer says where to check.
func TestPreflightOnARoomWithAHubRunsNoForge(t *testing.T) {
	d := testDaemon(t)
	f := &fakeExec{}
	f.install(t)
	d.SetHubForge(forge.NewRemote(nil))
	code, a := preflight(t, d, `{"forges":[{"tool":"gh","host":"github.com"},{"tool":"bb","host":"bitbucket.org"}]}`)
	if code != 200 {
		t.Fatalf("code %d", code)
	}
	if f.count() != 0 {
		t.Fatalf("a room with a hub ran %v", f.runs)
	}
	for _, k := range []string{"gh@github.com", "bb@bitbucket.org"} {
		s := a.Forges[k]
		if s.State != forgeUnknown || !strings.Contains(s.Message, "hub") {
			t.Fatalf("%s = %+v", k, s)
		}
	}
}
