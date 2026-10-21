//go:build integration

package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/claudeconf"
)

const codexPermissionPayload = `{"hook_event_name":"PermissionRequest","session_id":"codex-session",` +
	`"turn_id":"turn-1","cwd":"/work","tool_name":"Bash","tool_input":{"command":"echo hello"}}`

func TestInstalledCodexPermissionHookUsesNativeDecisions(t *testing.T) {
	for _, tc := range []struct{ name, answer, behavior, message string }{
		{"allow", `{"decision":"approve"}`, "allow", ""},
		{"deny", `{"decision":"block","reason":"standing rule"}`, "deny", "standing rule"},
		{"queued message", `{"decision":"block","reason":"Message from the human: test first"}`,
			"deny", "Message from the human: test first"},
		{"edit", `{"decision":"approve","command":"echo edited"}`, "deny",
			"atrium edited this request. retry with the edited input: echo edited"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			permEnv(t, "on")
			t.Setenv("ATRIUM_RUNNER", "codex-fast")
			h := newFakeHub(t, true, http.StatusOK, tc.answer)
			t.Setenv("ATRIUM_HUB_URL", h.srv.URL)
			lines := installedCommands(t, claudeconf.Codex)["PermissionRequest"]
			if len(lines) != 1 {
				t.Fatalf("native hooks: %v", lines)
			}
			defer withStdin(t, codexPermissionPayload)()
			root := newRoot()
			var out bytes.Buffer
			root.SetOut(&out)
			root.SetArgs(hookArgs(t, lines[0]))
			if err := root.Execute(); err != nil {
				t.Fatal(err)
			}
			var got struct {
				Output struct {
					Event    string `json:"hookEventName"`
					Decision struct {
						Behavior string `json:"behavior"`
						Message  string `json:"message"`
					} `json:"decision"`
				} `json:"hookSpecificOutput"`
			}
			if err := json.Unmarshal(out.Bytes(), &got); err != nil {
				t.Fatalf("%s: %v", out.String(), err)
			}
			if got.Output.Event != "PermissionRequest" || got.Output.Decision.Behavior != tc.behavior ||
				got.Output.Decision.Message != tc.message {
				t.Fatalf("native output: %s", out.String())
			}
			if strings.Contains(out.String(), "updatedInput") || strings.Contains(out.String(), "permissionDecision") {
				t.Fatalf("unsupported native fields: %s", out.String())
			}
			if len(h.posts) != 1 || h.posts[0]["runner"] != "codex-fast" || h.posts[0]["command"] != "echo hello" {
				t.Fatalf("requests: %+v", h.posts)
			}
			if h.posts[0]["dedup_key"] != "" {
				t.Fatalf("a request without a tool id can replay another attempt: %+v", h.posts[0])
			}
		})
	}
}

func TestCodexNativePermissionFailuresAreSilentAndNeverRetried(t *testing.T) {
	for _, answer := range []string{`{`, `{}`, `{"decision":"unknown"}`} {
		permEnv(t, "on")
		h := newFakeHub(t, true, http.StatusOK, answer)
		if got := runPermissionHook(h.srv.URL, []byte(codexPermissionPayload), 1); got != nil {
			t.Fatalf("failure must defer to Codex: %s", got)
		}
		if len(h.posts) != 1 {
			t.Fatalf("%d requests, want one", len(h.posts))
		}
	}
	permEnv(t, "on")
	failed := newFakeHub(t, true, http.StatusServiceUnavailable, `{"decision":"approve"}`)
	if got := runPermissionHook(failed.srv.URL, []byte(codexPermissionPayload), 1); got != nil || len(failed.posts) != 1 {
		t.Fatalf("HTTP failure must be silent with one attempt: %s, %d posts", got, len(failed.posts))
	}
	permEnv(t, "off")
	h := newFakeHub(t, true, http.StatusOK, `{"decision":"approve"}`)
	if got := runPermissionHook(h.srv.URL, []byte(codexPermissionPayload), 1); got != nil || len(h.posts) != 0 {
		t.Fatalf("disabled gate: %s, %d posts", got, len(h.posts))
	}
}

func TestCodexNativePermissionDoesNotSkipReadTools(t *testing.T) {
	permEnv(t, "on")
	h := newFakeHub(t, true, http.StatusOK, `{"decision":"block","reason":"network approval"}`)
	payload := strings.Replace(codexPermissionPayload, `"Bash"`, `"WebFetch"`, 1)
	if got := runPermissionHook(h.srv.URL, []byte(payload), 1); !bytes.Contains(got, []byte(`"behavior":"deny"`)) {
		t.Fatalf("native read approval bypassed chain: %s", got)
	}
}

func TestHookInstallSelectsCodexAndPrintsTrust(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	t.Setenv("ATRIUM_HOOK_EXE", fakeExe)
	h := newFakeHub(t, true, http.StatusOK, `{}`)
	t.Setenv("ATRIUM_HUB_URL", h.srv.URL)
	root := newRoot()
	var out bytes.Buffer
	root.SetOut(&out)
	root.SetArgs([]string{"hook", "install", "--runner", "codex"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	path, err := claudeconf.CodexHooksPath()
	if err != nil {
		t.Fatal(err)
	}
	rep, err := claudeconf.InspectTarget(claudeconf.Codex, fakeExe)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Path != filepath.ToSlash(path) || rep.Missing != 0 || !strings.Contains(out.String(), "trust") {
		t.Fatalf("install: %+v, %s", rep, out.String())
	}
	for _, hook := range rep.Hooks {
		if hook.Hook == "Stop" && hook.Installed {
			t.Fatal("install all opted into Stop")
		}
	}
}
