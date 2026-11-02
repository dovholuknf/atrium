//go:build integration

package cli

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHub stands in for the agent listener. gate is what /gate answers, perm
// is what /permission answers, and posts holds every /permission body.
type fakeHub struct {
	srv   *httptest.Server
	posts []map[string]any
}

func newFakeHub(t *testing.T, gate bool, permStatus int, permBody string) *fakeHub {
	t.Helper()
	h := &fakeHub{}
	mux := http.NewServeMux()
	mux.HandleFunc("/gate", func(w http.ResponseWriter, r *http.Request) {
		if gate {
			_, _ = w.Write([]byte(`{"gate":true}`))
		} else {
			_, _ = w.Write([]byte(`{"gate":false}`))
		}
	})
	mux.HandleFunc("/permission", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		h.posts = append(h.posts, m)
		w.WriteHeader(permStatus)
		_, _ = w.Write([]byte(permBody))
	})
	h.srv = httptest.NewServer(mux)
	t.Cleanup(h.srv.Close)
	return h
}

func permEnv(t *testing.T, gate string) {
	t.Helper()
	t.Setenv("ATRIUM_PERM_GATE", gate)
	t.Setenv("ATRIUM_AGENT_NAME", "tester")
	t.Setenv("ATRIUM_PERM_PROBE_TIMEOUT", "")
	t.Setenv("ATRIUM_PERM_TIMEOUT", "")
	t.Setenv("ATRIUM_LOCATION", "")
	// A cwd with no .mcp.json anywhere above it.
	t.Chdir(t.TempDir())
}

const bashPayload = `{"tool_name":"Bash","tool_use_id":"toolu_1","cwd":"/work/x","session_id":"s",` +
	`"tool_input":{"command":"ls -la"}}`

func TestPermOffAndForce(t *testing.T) {
	h := newFakeHub(t, false, 200, `{"decision":"approve"}`)
	permEnv(t, "off")
	if out := runPermissionHook(h.srv.URL, []byte(bashPayload), 1); out != nil || len(h.posts) != 0 {
		t.Fatalf("off must be silent and not post, got %q, %d posts", out, len(h.posts))
	}
	permEnv(t, "force")
	if out := runPermissionHook(h.srv.URL, []byte(bashPayload), 1); out == nil || len(h.posts) != 1 {
		t.Fatalf("force must post, got %q, %d posts", out, len(h.posts))
	}
}

func TestPermUnsetNeedsJoinOrWire(t *testing.T) {
	h := newFakeHub(t, false, 200, `{"decision":"approve"}`)
	permEnv(t, "")
	if out := runPermissionHook(h.srv.URL, []byte(bashPayload), 1); out != nil || len(h.posts) != 0 {
		t.Fatalf("not joined, not wired must be silent")
	}
	h2 := newFakeHub(t, true, 200, `{"decision":"approve"}`)
	if out := runPermissionHook(h2.srv.URL, []byte(bashPayload), 1); out == nil || len(h2.posts) != 1 {
		t.Fatalf("joined must post")
	}
}

func TestPermSkippedToolNeverPosts(t *testing.T) {
	h := newFakeHub(t, true, 200, `{"decision":"approve"}`)
	permEnv(t, "force")
	for tool := range permSkipTools {
		p := `{"tool_name":"` + tool + `","tool_input":{"file_path":"/a"}}`
		if out := runPermissionHook(h.srv.URL, []byte(p), 1); out != nil {
			t.Fatalf("%s produced output", tool)
		}
	}
	if len(h.posts) != 0 {
		t.Fatalf("a skipped tool posted")
	}
}

func TestPermSkipsBothSubagentToolNames(t *testing.T) {
	h := newFakeHub(t, true, 200, `{"decision":"approve"}`)
	permEnv(t, "force")
	for _, tool := range []string{"Task", "Agent"} {
		if !permSkipTools[tool] {
			t.Fatalf("%s is not in permSkipTools", tool)
		}
		p := `{"tool_name":"` + tool + `","tool_input":{"prompt":"x"}}`
		if out := runPermissionHook(h.srv.URL, []byte(p), 1); out != nil {
			t.Fatalf("%s produced output", tool)
		}
	}
	if len(h.posts) != 0 {
		t.Fatalf("a subagent tool posted")
	}
}

func TestPermProbeNotGatedAndTimeout(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real work")
	}
	// Answers "not gated" and there is no .mcp.json.
	h := newFakeHub(t, false, 200, `{"decision":"approve"}`)
	permEnv(t, "")
	if out := runPermissionHook(h.srv.URL, []byte(bashPayload), 1); out != nil || len(h.posts) != 0 {
		t.Fatalf("not gated must be silent")
	}

	// A frozen /gate must not hold the hook past the probe deadline.
	release := make(chan struct{})
	frozen := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-release
	}))
	t.Cleanup(frozen.Close)
	t.Cleanup(func() { close(release) })
	permEnv(t, "force")
	t.Setenv("ATRIUM_PERM_PROBE_TIMEOUT", "1")
	start := time.Now()
	out := string(runPermissionHook(frozen.URL, []byte(bashPayload), 1))
	if !strings.Contains(out, "systemMessage") || strings.Contains(out, "permissionDecision") {
		t.Fatalf("frozen probe must fail open with a warning, got %q", out)
	}
	if d := time.Since(start); d > 2500*time.Millisecond {
		t.Fatalf("hook held %v past a 1s probe deadline", d)
	}
}

func TestPermWiredByMCPJSON(t *testing.T) {
	h := newFakeHub(t, false, 200, `{"decision":"approve"}`)
	permEnv(t, "")
	if err := writeFileT(".mcp.json", `{"mcpServers":{"atrium-agent":{}}}`); err != nil {
		t.Fatal(err)
	}
	if out := runPermissionHook(h.srv.URL, []byte(bashPayload), 1); out == nil || len(h.posts) != 1 {
		t.Fatalf("a wired cwd must post")
	}
}

func TestPermPostCarriesSummaryPerTool(t *testing.T) {
	cases := []struct {
		name, payload, command, details string
	}{
		{"bash", `{"tool_name":"Bash","tool_use_id":"u1","cwd":"/w","tool_input":{"command":"ls"}}`, "ls", ""},
		{"powershell", `{"tool_name":"PowerShell","tool_use_id":"u1","cwd":"/w","tool_input":{"command":"gci"}}`, "gci", ""},
		{"edit", `{"tool_name":"Edit","tool_use_id":"u1","cwd":"/w","tool_input":{"file_path":"/a.go","old_string":"x","new_string":"y"}}`,
			"/a.go <- (replace edit)", "--- removing\nx\n\n+++ adding\ny"},
		{"write", `{"tool_name":"Write","tool_use_id":"u1","cwd":"/w","tool_input":{"file_path":"/a.go","content":"héllo"}}`,
			"/a.go <- (write 5 chars)", "+++ writing\nhéllo"},
		{"url", `{"tool_name":"Foo","tool_use_id":"u1","cwd":"/w","tool_input":{"url":"http://x"}}`, "http://x", ""},
		{"pattern", `{"tool_name":"Foo","tool_use_id":"u1","cwd":"/w","tool_input":{"pattern":"a*"}}`, "a*", ""},
		{"json", `{"tool_name":"Foo","tool_use_id":"u1","cwd":"/w","tool_input":{"a": 1, "b": "z"}}`, `{"a":1,"b":"z"}`, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			h := newFakeHub(t, true, 200, `{"decision":"approve"}`)
			permEnv(t, "force")
			if out := runPermissionHook(h.srv.URL, []byte(c.payload), 4242); out == nil {
				t.Fatal("no output")
			}
			if len(h.posts) != 1 {
				t.Fatalf("%d posts", len(h.posts))
			}
			p := h.posts[0]
			if p["dedup_key"] != "u1" || p["tool_use_id"] != "u1" {
				t.Errorf("dedup key not the tool_use_id: %v", p)
			}
			if p["pid"] != float64(4242) || p["cwd"] != "/w" || p["agent"] != "tester" {
				t.Errorf("pid, cwd or agent wrong: %v", p)
			}
			if p["command"] != c.command {
				t.Errorf("command = %q, want %q", p["command"], c.command)
			}
			if got, _ := p["details"].(string); got != c.details {
				t.Errorf("details = %q, want %q", got, c.details)
			}
		})
	}
}

func TestPermDetailsTruncated(t *testing.T) {
	d := permDetails(map[string]any{"content": strings.Repeat("a", 7000)})
	if !strings.Contains(d, "... truncated, ") || len(d) > 6200 {
		t.Fatalf("not truncated: %d", len(d))
	}
}

func TestPermDecisions(t *testing.T) {
	parse := func(t *testing.T, out []byte) map[string]any {
		t.Helper()
		var m struct {
			H map[string]any `json:"hookSpecificOutput"`
		}
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatalf("output %q: %v", out, err)
		}
		return m.H
	}
	t.Run("allow", func(t *testing.T) {
		h := newFakeHub(t, true, 200, `{"decision":"approve"}`)
		permEnv(t, "force")
		o := parse(t, runPermissionHook(h.srv.URL, []byte(bashPayload), 1))
		if o["permissionDecision"] != "allow" || o["hookEventName"] != "PreToolUse" ||
			o["permissionDecisionReason"] != "via atrium hub" || o["updatedInput"] != nil {
			t.Fatalf("%v", o)
		}
	})
	t.Run("deny with reason", func(t *testing.T) {
		h := newFakeHub(t, true, 200, `{"decision":"block","reason":"no way"}`)
		permEnv(t, "force")
		o := parse(t, runPermissionHook(h.srv.URL, []byte(bashPayload), 1))
		if o["permissionDecision"] != "deny" || o["permissionDecisionReason"] != "no way" {
			t.Fatalf("%v", o)
		}
	})
	t.Run("edited command", func(t *testing.T) {
		h := newFakeHub(t, true, 200, `{"decision":"approve","command":"ls -l"}`)
		permEnv(t, "force")
		p := `{"tool_name":"Bash","tool_use_id":"u","tool_input":{"command":"ls -la","timeout":5}}`
		o := parse(t, runPermissionHook(h.srv.URL, []byte(p), 1))
		u, _ := o["updatedInput"].(map[string]any)
		if o["permissionDecision"] != "allow" || u["command"] != "ls -l" || u["timeout"] != float64(5) {
			t.Fatalf("%v", o)
		}
	})
	t.Run("edited file path drops the note", func(t *testing.T) {
		h := newFakeHub(t, true, 200, `{"decision":"approve","command":"/b.go <- (replace edit)"}`)
		permEnv(t, "force")
		p := `{"tool_name":"Edit","tool_use_id":"u","tool_input":{"file_path":"/a.go","old_string":"x","new_string":"y"}}`
		o := parse(t, runPermissionHook(h.srv.URL, []byte(p), 1))
		u, _ := o["updatedInput"].(map[string]any)
		if u["file_path"] != "/b.go" || u["new_string"] != "y" {
			t.Fatalf("%v", o)
		}
	})
	t.Run("an edit that does not parse is a deny", func(t *testing.T) {
		for name, p := range map[string]string{
			"mcp call":    `{"tool_name":"mcp__x__do","tool_use_id":"u","tool_input":{"a":1,"b":"z"}}`,
			"apply_patch": `{"tool_name":"apply_patch","tool_use_id":"u","tool_input":{"patch":"*** Begin Patch"}}`,
		} {
			h := newFakeHub(t, true, 200, `{"decision":"approve","command":"{\"a\":1,"}`)
			permEnv(t, "force")
			o := parse(t, runPermissionHook(h.srv.URL, []byte(p), 1))
			if o["permissionDecision"] != "deny" ||
				o["permissionDecisionReason"] != "the edit did not parse, nothing was run" ||
				o["updatedInput"] != nil {
				t.Fatalf("%s: %v", name, o)
			}
		}
	})
	t.Run("an edit that parses still allows", func(t *testing.T) {
		h := newFakeHub(t, true, 200, `{"decision":"approve","command":"{\"a\":2}"}`)
		permEnv(t, "force")
		p := `{"tool_name":"mcp__x__do","tool_use_id":"u","tool_input":{"a":1}}`
		o := parse(t, runPermissionHook(h.srv.URL, []byte(p), 1))
		u, _ := o["updatedInput"].(map[string]any)
		if o["permissionDecision"] != "allow" || u["a"] != float64(2) {
			t.Fatalf("%v", o)
		}
	})
	t.Run("a block never rewrites", func(t *testing.T) {
		h := newFakeHub(t, true, 200, `{"decision":"block","command":"ls -l"}`)
		permEnv(t, "force")
		o := parse(t, runPermissionHook(h.srv.URL, []byte(bashPayload), 1))
		if o["updatedInput"] != nil {
			t.Fatalf("%v", o)
		}
	})
}

func TestPermFailuresFailOpen(t *testing.T) {
	permEnv(t, "force")
	shortPermRetry(t, 300*time.Millisecond)
	for name, c := range map[string]struct {
		status int
		body   string
		stdin  string
	}{
		"post fails":      {500, `boom`, bashPayload},
		"bad answer":      {200, `not json`, bashPayload},
		"unknown verdict": {200, `{"decision":"maybe"}`, bashPayload},
		"malformed stdin": {200, `{"decision":"approve"}`, `{not json`},
		"empty stdin":     {200, `{"decision":"approve"}`, ``},
	} {
		t.Run(name, func(t *testing.T) {
			h := newFakeHub(t, true, c.status, c.body)
			if out := runPermissionHook(h.srv.URL, []byte(c.stdin), 1); out != nil {
				t.Fatalf("expected silence, got %q", out)
			}
		})
	}
	// Nothing listening at all: no decision, and the warning says where.
	if out := string(runPermissionHook("http://127.0.0.1:1", []byte(bashPayload), 1)); !strings.Contains(out, "http://127.0.0.1:1") ||
		strings.Contains(out, "permissionDecision") {
		t.Fatalf("expected the warning alone, got %q", out)
	}
}

func TestPermPostTimeoutEnv(t *testing.T) {
	for in, want := range map[string]time.Duration{
		"": 0, "30": 30 * time.Second, "2m": 2 * time.Minute, "00:01:30": 90 * time.Second, "junk": 0,
	} {
		t.Setenv("ATRIUM_PERM_TIMEOUT", in)
		if got := permPostTimeout(); got != want {
			t.Errorf("%q: %v, want %v", in, got, want)
		}
	}
}

func writeFileT(name, body string) error { return os.WriteFile(name, []byte(body), 0o644) }

// r-042: the process table is walked only when there is something to post.
func TestPermissionHookFindsThePidOnlyWhenPosting(t *testing.T) {
	h := newFakeHub(t, true, 200, `{"decision":"approve"}`)
	calls := 0
	pidOf := func() int { calls++; return 4242 }

	permEnv(t, "off")
	permissionHook(h.srv.URL, []byte(bashPayload), pidOf)
	permEnv(t, "force")
	permissionHook(h.srv.URL, nil, pidOf)
	permissionHook(h.srv.URL, []byte(`not json`), pidOf)
	permissionHook(h.srv.URL, []byte(`{"tool_name":"Read","tool_input":{}}`), pidOf)
	if calls != 0 {
		t.Fatalf("the pid was looked up %d times for calls with nothing to do", calls)
	}
	// The room has no gate and nothing forces one: nothing to post either.
	quiet := newFakeHub(t, false, 200, `{"decision":"approve"}`)
	permEnv(t, "")
	permissionHook(quiet.srv.URL, []byte(bashPayload), pidOf)
	if calls != 0 {
		t.Fatalf("the pid was looked up for an ungated room")
	}
	if out := permissionHook(h.srv.URL, []byte(bashPayload), pidOf); out == nil || calls != 1 {
		t.Fatalf("a post must look the pid up once: out=%q calls=%d", out, calls)
	}
	if got := h.posts[len(h.posts)-1]["pid"]; got != float64(4242) {
		t.Fatalf("pid posted %v", got)
	}
}

// A whereami left pointing at a dead room must not hold a hook: the probe is
// refused or times out and the hook prints only the warning.
func TestPermissionHookFailsOpenOnADeadAddress(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	t.Setenv("ATRIUM_PERM_GATE", "on")
	t.Setenv("ATRIUM_PERM_PROBE_TIMEOUT", "1")

	start := time.Now()
	out := string(runPermissionHook("http://"+addr, []byte(`{"tool_name":"Bash","tool_input":{"command":"ls"}}`), 1))
	if !strings.Contains(out, `"additionalContext"`) || !strings.Contains(out, addr) || strings.Contains(out, "permissionDecision") {
		t.Fatalf("expected the warning alone, got %s", out)
	}
	if time.Since(start) > 4*time.Second {
		t.Fatalf("the hook waited %s on a dead address", time.Since(start))
	}
}

// shortPermRetry makes the retry bound a test's own, and puts it back.
func shortPermRetry(t *testing.T, bound time.Duration) {
	t.Helper()
	t.Setenv("ATRIUM_PERM_RETRY_FOR", bound.String())
	t.Setenv("ATRIUM_PERM_RETRY_EVERY", "50ms")
}

// A restart resets the long poll a couple of times and then the new room decides: the hook gets that decision, with
// the same dedup key each time. A room that never answers fails open after the bound and records nothing.
func TestPermissionHookRetriesThroughARestart(t *testing.T) {
	permEnv(t, "force")
	shortPermRetry(t, 2*time.Second)

	var mu sync.Mutex
	var keys []string
	posts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/gate", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"gate":true}`)) })
	mux.HandleFunc("/permission", func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var m map[string]any
		_ = json.Unmarshal(raw, &m)
		mu.Lock()
		posts++
		n := posts
		keys = append(keys, m["dedup_key"].(string))
		mu.Unlock()
		if n <= 2 {
			// Reset, as a daemon exiting under the request does.
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"decision":"block","reason":"no"}`))
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	out := runPermissionHook(srv.URL, []byte(bashPayload), 1)
	if out == nil || !strings.Contains(string(out), `"deny"`) {
		t.Fatalf("expected the decision after two resets, got %q", out)
	}
	mu.Lock()
	if posts != 3 || keys[0] != keys[1] || keys[1] != keys[2] || keys[0] != "toolu_1" {
		t.Fatalf("posts %d keys %v, want three with one key", posts, keys)
	}
	mu.Unlock()

	// Never answers: silent after the bound.
	never := newFakeHub(t, true, 503, `down`)
	start := time.Now()
	if out := runPermissionHook(never.srv.URL, []byte(bashPayload), 1); out != nil {
		t.Fatalf("expected silence, got %q", out)
	}
	if took := time.Since(start); took < time.Second || took > 4*time.Second {
		t.Fatalf("gave up after %s, want about the 2s bound", took)
	}
	if len(never.posts) < 3 {
		t.Fatalf("retried %d times", len(never.posts))
	}
}

// A room that answers /gate and then never answers /permission, as one starting up after a restart does, lets the
// call through within the bound. A room that parks the request sends its headers first and is waited on.
func TestPermissionHookFailsOpenOnARoomThatDoesNotAnswer(t *testing.T) {
	permEnv(t, "force")
	shortPermRetry(t, 300*time.Millisecond)
	t.Setenv("ATRIUM_PERM_HEADER_TIMEOUT", "200ms")

	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/gate", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"gate":true}`)) })
	mux.HandleFunc("/permission", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	})
	mute := httptest.NewServer(mux)
	defer mute.Close()
	// Released before Close, which waits on the handler: an unread body never cancels its context.
	defer close(release)

	start := time.Now()
	if out := string(runPermissionHook(mute.URL, []byte(bashPayload), 1)); !strings.Contains(out, "systemMessage") ||
		strings.Contains(out, "permissionDecision") {
		t.Fatalf("expected the warning alone, got %q", out)
	}
	if took := time.Since(start); took > 2*time.Second {
		t.Fatalf("the hook waited %s on a room that does not answer", took)
	}

	parked := http.NewServeMux()
	parked.HandleFunc("/gate", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(`{"gate":true}`)) })
	parked.HandleFunc("/permission", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_ = http.NewResponseController(w).Flush()
		time.Sleep(time.Second) // wait: a human slower than the header bound
		_, _ = w.Write([]byte(`{"decision":"approve"}`))
	})
	human := httptest.NewServer(parked)
	defer human.Close()
	if out := runPermissionHook(human.URL, []byte(bashPayload), 1); out == nil || !strings.Contains(string(out), `"allow"`) {
		t.Fatalf("a parked request must wait past the header bound for its decision, got %q", out)
	}
}

func TestPermissionHookSaysWhichCardItsRunnerIs(t *testing.T) {
	h := newFakeHub(t, true, 200, `{"decision":"approve"}`)
	permEnv(t, "force")
	t.Setenv("ATRIUM_TASK_ID", "card-1")
	permissionHook(h.srv.URL, []byte(bashPayload), func() int { return 4242 })
	if len(h.posts) != 1 || h.posts[0]["task_id"] != "card-1" {
		t.Fatalf("posted %v, want task_id card-1", h.posts)
	}
}
