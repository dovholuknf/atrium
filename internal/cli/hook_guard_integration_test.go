//go:build integration

package cli

import (
	"encoding/json"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func guardCall(tool string, input map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{"tool_name": tool, "cwd": "D:/w", "tool_input": input})
	return b
}

func guardDecision(t *testing.T, out []byte) string {
	t.Helper()
	if out == nil {
		return ""
	}
	var v struct {
		H struct {
			Event    string `json:"hookEventName"`
			Decision string `json:"permissionDecision"`
			Reason   string `json:"permissionDecisionReason"`
		} `json:"hookSpecificOutput"`
	}
	if err := json.Unmarshal(out, &v); err != nil {
		t.Fatalf("%s: %v", out, err)
	}
	if v.H.Event != "PreToolUse" || v.H.Reason == "" {
		t.Fatalf("bad output %s", out)
	}
	return v.H.Decision
}

func TestGuardHookOffByDefault(t *testing.T) {
	deny := guardCall("Agent", map[string]any{"subagent_type": "general-purpose"})
	for _, v := range []string{"", "off", "0", "no", "nope"} {
		env := guardFakeEnv{vars: map[string]string{"ATRIUM_GUARD": v}}
		if out := guardHook(deny, env); out != nil {
			t.Errorf("ATRIUM_GUARD=%q printed %s", v, out)
		}
	}
}

func TestGuardHookBuiltin(t *testing.T) {
	env := guardFakeEnv{vars: map[string]string{"ATRIUM_GUARD": "on"}}
	for _, c := range []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Agent", map[string]any{"subagent_type": "general-purpose"}, "deny"},
		{"Bash", map[string]any{"command": "echo a; echo b"}, "deny"},
		{"Bash", map[string]any{"command": "echo hi 2>&1"}, ""},
		{"Read", map[string]any{"file_path": "D:/w/x"}, ""},
	} {
		if got := guardDecision(t, guardHook(guardCall(c.tool, c.input), env)); got != c.want {
			t.Errorf("%s %v: %q, want %q", c.tool, c.input, got, c.want)
		}
	}
	for _, in := range [][]byte{nil, []byte("not json"), []byte(`{"tool_input":"x"}`)} {
		if out := guardHook(in, env); out != nil {
			t.Errorf("%q printed %s", in, out)
		}
	}
}

func TestGuardHookOverride(t *testing.T) {
	// Native paths, so filepath.Clean in the guard reads them as this OS does.
	path := filepath.FromSlash("/rules/guard.json")
	good := `{"rules":[{"id":"no-echo","check":"command","tools":["shell"],"reason":"no {name}","params":{"names":["echo"]}}]}`
	env := guardFakeEnv{vars: map[string]string{"ATRIUM_GUARD": "yes", guardRulesEnv: path},
		files: map[string]string{path: good}}
	if got := guardDecision(t, guardHook(guardCall("Bash", map[string]any{"command": "echo hi"}), env)); got != "deny" {
		t.Errorf("override not used: %q", got)
	}
	// The override replaces the built-in rules, so a general-purpose agent is fine.
	if got := guardDecision(t, guardHook(guardCall("Agent", map[string]any{"subagent_type": "general-purpose"}), env)); got != "" {
		t.Errorf("built-in rules still applied: %q", got)
	}

	env.files[path] = `{"rules":[{"id":"x","check":"no-such-check","tools":["shell"],"reason":"r"}]}`
	// Another spelling of the file is the file only where the file system ignores case.
	otherCase := "deny"
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		otherCase = ""
	}
	for _, c := range []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "echo hi"}, "deny"},
		{"PowerShell", map[string]any{"command": "Get-Date"}, "deny"},
		{"Agent", map[string]any{"subagent_type": "x"}, "deny"},
		{"Write", map[string]any{"file_path": filepath.FromSlash("/other.json"), "content": "{}"}, "deny"},
		{"Write", map[string]any{"file_path": path, "content": "{}"}, ""},
		{"Write", map[string]any{"file_path": filepath.FromSlash("/RULES/guard.json"), "content": "{}"}, otherCase},
		{"Edit", map[string]any{"file_path": filepath.FromSlash("/rules/./guard.json"), "new_string": "{}"}, ""},
		{"Read", map[string]any{"file_path": path}, ""},
	} {
		out := guardHook(guardCall(c.tool, c.input), env)
		if got := guardDecision(t, out); got != c.want {
			t.Errorf("broken rules, %s %v: %q, want %q", c.tool, c.input, got, c.want)
		}
		if c.want == "deny" && !strings.Contains(string(out), "guard.json") {
			t.Errorf("reason does not name the file: %s", out)
		}
	}

	delete(env.files, path)
	if got := guardDecision(t, guardHook(guardCall("Bash", map[string]any{"command": "echo hi"}), env)); got != "deny" {
		t.Errorf("missing override file: %q, want deny", got)
	}
}
