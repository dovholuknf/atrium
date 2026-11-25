//go:build integration

package daemon

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

func TestCursorPluginCarriesAtriumControl(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "cursor-plugin")
	for range 2 {
		if err := writeCursorPlugin(dir, filepath.Join("C:", "tools", "atrium.exe")); err != nil {
			t.Fatal(err)
		}
	}
	var manifest struct{ Name string }
	raw, err := os.ReadFile(filepath.Join(dir, ".cursor-plugin", "plugin.json"))
	if err != nil || json.Unmarshal(raw, &manifest) != nil || manifest.Name != "atrium" {
		t.Fatalf("manifest %s, %v", raw, err)
	}
	var doc struct {
		MCPServers map[string]struct {
			Command string
			Args    []string
			Env     map[string]string
		} `json:"mcpServers"`
	}
	raw, err = os.ReadFile(filepath.Join(dir, ".mcp.json"))
	if err != nil || json.Unmarshal(raw, &doc) != nil {
		t.Fatalf("mcp %s, %v", raw, err)
	}
	s := doc.MCPServers["atrium-control"]
	if !strings.HasSuffix(s.Command, "tools/atrium.exe") || strings.Join(s.Args, " ") != "control" {
		t.Fatalf("atrium-control is %+v", s)
	}
	if s.Env["ATRIUM_AGENT_NAME"] != "${env:ATRIUM_AGENT_NAME}" {
		t.Fatalf("cursor starts the server bare, so the card's name has to be named: %+v", s.Env)
	}
}

func TestCodexHooksAreWiredIntoTheRowsCodexHome(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	exe := filepath.Join(home, "bin", "atrium.exe")
	ok, err := ensureCodexHooks(home, exe)
	if err != nil || !ok {
		t.Fatalf("first wire: %v, %v", ok, err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "hooks.json"))
	if err != nil || !strings.Contains(string(raw), "session --event start --runner codex") {
		t.Fatalf("hooks.json %s, %v", raw, err)
	}
	other := filepath.Join(home, "other-room", "atrium.exe")
	if ok, err := ensureCodexHooks(home, other); err != nil || !ok {
		t.Fatalf("another room's binary: %v, %v", ok, err)
	}
	again, _ := os.ReadFile(filepath.Join(home, "hooks.json"))
	if string(again) != string(raw) {
		t.Fatalf("another room rewrote hooks that were already there:\n%s", again)
	}
	if err := os.WriteFile(filepath.Join(home, "hooks.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if ok, err := ensureCodexHooks(home, exe); ok || err == nil {
		t.Fatalf("broken hooks.json: %v, %v", ok, err)
	}
}

func TestCursorPluginCarriesAtriumsHooks(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "cursor-plugin")
	if err := writeCursorPlugin(dir, filepath.Join("C:", "tools", "atrium.exe")); err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Version int
		Hooks   map[string][]struct {
			Command string
			Timeout int
		}
	}
	raw, err := os.ReadFile(filepath.Join(dir, "hooks", "hooks.json"))
	if err != nil || json.Unmarshal(raw, &doc) != nil || doc.Version != 1 {
		t.Fatalf("hooks %s, %v", raw, err)
	}
	for event, line := range map[string]string{"sessionStart": "session --event start --runner cursor",
		"beforeSubmitPrompt": "hook --event prompt --runner cursor", "stop": "turn --event end --runner cursor",
		"sessionEnd": "session --event end --runner cursor"} {
		if len(doc.Hooks[event]) != 1 || !strings.HasSuffix(doc.Hooks[event][0].Command, "tools/atrium.exe "+line) {
			t.Errorf("%s runs %+v, want %q", event, doc.Hooks[event], line)
		}
	}
	gated := false
	for _, h := range doc.Hooks["preToolUse"] {
		if strings.HasSuffix(h.Command, "hook --event permission --runner cursor") {
			gated = h.Timeout == 86400
		}
	}
	if !gated {
		t.Fatalf("preToolUse has no gate that waits a day: %+v", doc.Hooks["preToolUse"])
	}
}

func TestCopilotHooksAreWiredIntoTheRowsCopilotHome(t *testing.T) {
	t.Parallel()
	home := t.TempDir()
	exe := filepath.Join(home, "bin", "atrium.exe")
	if ok, err := ensureHooks(claudeconf.CopilotAt(home), exe); err != nil || !ok {
		t.Fatalf("wire: %v, %v", ok, err)
	}
	raw, err := os.ReadFile(filepath.Join(home, "hooks", "atrium.json"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"session --event start --runner copilot", "turn --event end --runner copilot",
		"hook --event permission --runner copilot", `"timeout": 86400`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("atrium.json has no %q:\n%s", want, raw)
		}
	}
}
