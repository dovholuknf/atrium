package cli

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
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

func TestPermProbeNotGatedAndTimeout(t *testing.T) {
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
	out := runPermissionHook(frozen.URL, []byte(bashPayload), 1)
	if out != nil {
		t.Fatalf("frozen probe must fail open, got %q", out)
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
	// Nothing listening at all.
	if out := runPermissionHook("http://127.0.0.1:1", []byte(bashPayload), 1); out != nil {
		t.Fatalf("expected silence, got %q", out)
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
