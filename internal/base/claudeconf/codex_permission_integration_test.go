//go:build integration

package claudeconf

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

func TestCodexInstallIncludesNativeGateWithHumanTimeout(t *testing.T) {
	path := withCodexHome(t, "")
	if _, _, err := InstallOnlyTarget(Codex, "/opt/atrium", nil); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	cmds := registeredCommands(doc)
	if len(cmds["PermissionRequest"]) != 1 {
		t.Fatalf("native gate: %s", raw)
	}
	command := cmds["PermissionRequest"][0]
	if !strings.Contains(command, "hook --event permission --runner codex") ||
		timeoutOf(doc, "PermissionRequest", command) != gateTimeout {
		t.Fatalf("native gate: %s", raw)
	}
	for _, command := range cmds["PreToolUse"] {
		if strings.Contains(command, "--event permission") {
			t.Fatalf("double gate: %s", raw)
		}
	}
	if len(cmds["PreToolUse"]) != 1 || len(cmds["PostToolUse"]) != 1 || len(cmds["Stop"]) != 0 {
		t.Fatalf("activity hooks or optional Stop changed: %s", raw)
	}
}
