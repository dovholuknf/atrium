package cli

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// guardFakeEnv answers the guard without touching the machine.
type guardFakeEnv struct {
	vars  map[string]string
	files map[string]string
}

func (guardFakeEnv) Git(string, ...string) (string, error) { return "", errors.New("no git here") }
func (guardFakeEnv) Stat(string) (bool, bool)              { return false, false }
func (guardFakeEnv) HasPrefix(string, string) bool         { return false }
func (e guardFakeEnv) ReadFile(p string) ([]byte, error) {
	if s, ok := e.files[p]; ok {
		return []byte(s), nil
	}
	return nil, errors.New("no such file")
}
func (e guardFakeEnv) Getenv(k string) string { return e.vars[k] }
func (guardFakeEnv) Agent() string            { return "" }

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
	const path = `D:\rules\guard.json`
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
	for _, c := range []struct {
		tool  string
		input map[string]any
		want  string
	}{
		{"Bash", map[string]any{"command": "echo hi"}, "deny"},
		{"PowerShell", map[string]any{"command": "Get-Date"}, "deny"},
		{"Agent", map[string]any{"subagent_type": "x"}, "deny"},
		{"Write", map[string]any{"file_path": `D:\other.json`, "content": "{}"}, "deny"},
		{"Write", map[string]any{"file_path": `d:\rules\guard.json`, "content": "{}"}, ""},
		{"Edit", map[string]any{"file_path": `D:\rules\.\guard.json`, "new_string": "{}"}, ""},
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
