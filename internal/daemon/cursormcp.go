package daemon

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// isCursor reports whether a runner row runs the cursor agent CLI.
func isCursor(h *store.Harness) bool {
	return shared.RunsAs(h, "cursor-agent")
}

// cursorHooks are cursor's own event names and the atrium hook line behind each. Cursor 2026.11.22 reads
// hooks/hooks.json from a --plugin-dir plugin in its own shape, and runs claude's ~/.claude/settings.json hooks too,
// which the CLI skips for a cursor payload so nothing is reported twice.
var cursorHooks = []struct{ event, line string }{
	{"sessionStart", "session --event start"},
	{"sessionEnd", "session --event end"},
	{"preCompact", "session --event compact"},
	{"beforeSubmitPrompt", "hook --event prompt"},
	{"preToolUse", "hook --event tool-start"},
	{"preToolUse", "hook --event permission"},
	{"postToolUse", "hook --event tool-end"},
	{"postToolUseFailure", "hook --event tool-failed"},
	{"subagentStart", "hook --event subagent-start"},
	{"subagentStop", "hook --event subagent-end"},
	{"stop", "turn --event end"},
}

// cursorHooksDoc is the plugin's hooks/hooks.json. The gate waits on a person, so it carries a day as its timeout.
func cursorHooksDoc(exe string) map[string]any {
	prog := filepath.ToSlash(exe)
	if strings.ContainsAny(prog, " \t") {
		prog = `"` + prog + `"`
		// Cursor runs a hook command in PowerShell on Windows, which reads a quoted string as a string.
		if runtime.GOOS == "windows" {
			prog = "& " + prog
		}
	}
	hooks := map[string][]map[string]any{}
	for _, h := range cursorHooks {
		entry := map[string]any{"command": prog + " " + h.line + " --runner cursor"}
		if strings.HasSuffix(h.line, "permission") {
			entry["timeout"] = 86400
		}
		hooks[h.event] = append(hooks[h.event], entry)
	}
	return map[string]any{"version": 1, "hooks": hooks}
}

// cursorControlEnv hands the card's identity to the MCP server, which cursor starts with a bare environment.
func cursorControlEnv() map[string]string {
	env := map[string]string{}
	for _, k := range codexControlEnv {
		env[k] = "${env:" + k + "}"
	}
	return env
}

// writeCursorPlugin makes dir a cursor plugin carrying atrium-control and atrium's hooks, so a launch can name it
// with --plugin-dir instead of atrium writing into a card's checkout or the operator's home.
func writeCursorPlugin(dir, exe string) error {
	manifest, _ := json.MarshalIndent(map[string]any{"name": "atrium"}, "", "  ")
	servers, _ := json.MarshalIndent(map[string]any{"mcpServers": map[string]any{
		"atrium-control": map[string]any{"command": filepath.ToSlash(exe), "args": []string{"control"}, "env": cursorControlEnv()},
	}}, "", "  ")
	hooks, _ := json.MarshalIndent(cursorHooksDoc(exe), "", "  ")
	for path, body := range map[string][]byte{
		filepath.Join(dir, ".cursor-plugin", "plugin.json"): append(manifest, '\n'),
		filepath.Join(dir, ".mcp.json"):                     append(servers, '\n'),
		filepath.Join(dir, "hooks", "hooks.json"):           append(hooks, '\n'),
	} {
		if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, body) {
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := os.WriteFile(path, body, 0o600); err != nil {
			return err
		}
	}
	return nil
}

// withCursorLaunch adds --trust, since cursor otherwise stops at a workspace trust dialog nobody is watching, and the
// atrium-control plugin with its servers approved. A flag the row or the launch already passes is not added twice.
// force adds --force for a launch whose tool calls go through atrium's gate: cursor takes a hook's deny but never its
// allow, so only with its own prompt off can the board be the one that approves.
func withCursorLaunch(args, own []string, plugin string, force bool) []string {
	has := func(flag string) bool { return slices.Contains(args, flag) || slices.Contains(own, flag) }
	out := append([]string{}, args...)
	if !has("--trust") {
		out = append(out, "--trust")
	}
	if force && plugin != "" && !has("--force") && !has("--yolo") && !has("-f") {
		out = append(out, "--force")
	}
	if plugin == "" {
		return out
	}
	out = append(out, "--plugin-dir", filepath.ToSlash(plugin))
	if !has("--approve-mcps") {
		out = append(out, "--approve-mcps")
	}
	return out
}

// gatedLaunch says a launch with this environment routes its tool calls through atrium's gate: ATRIUM_PERM_GATE
// unset, which permGateDefault makes on, or set to one of the words the permission hook reads as on.
func gatedLaunch(env map[string]string) bool {
	for k, v := range env {
		if strings.EqualFold(k, "ATRIUM_PERM_GATE") {
			switch strings.ToLower(strings.TrimSpace(v)) {
			case "on", "force", "1", "true", "yes":
				return true
			}
			return false
		}
	}
	return true
}
