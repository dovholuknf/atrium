package daemon

import (
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dovholuknf/atrium/internal/base/claudeconf"
)

const codexControlKey = "mcp_servers.atrium-control"

// codexControlEnv is what `atrium control` reads to know its card and room. Codex hands an MCP server only the
// variables it is told to.
var codexControlEnv = []string{"ATRIUM_AGENT_NAME", "ATRIUM_TASK_ID", "ATRIUM_ROOM", "ATRIUM_LOCATION", "ATRIUM_CARD",
	"ATRIUM_RUNNER", "ATRIUM_HUB_URL"}

// withCodexControlMCP adds atrium-control as `-c` overrides, the way a claude row names its --mcp-config, unless the
// row or the launch already configures it. Single-quoted TOML, so a codex batch shim never sees a double quote.
func withCodexControlMCP(args, own []string, exe string) []string {
	exe = filepath.ToSlash(exe)
	if strings.Contains(exe, "'") {
		return args
	}
	for _, a := range append(append([]string{}, args...), own...) {
		if strings.Contains(a, codexControlKey) {
			return args
		}
	}
	return append(append([]string{}, args...),
		"-c", codexControlKey+".command='"+exe+"'",
		"-c", codexControlKey+".args=['control']",
		"-c", codexControlKey+".env_vars=['"+strings.Join(codexControlEnv, "','")+"']",
		"-c", codexControlKey+".default_tools_approval_mode='approve'")
}

const codexHookTrustFlag = "--dangerously-bypass-hook-trust"

// withCodexHookTrust lets codex run atrium's hooks, which it otherwise skips until a human has reviewed them once.
func withCodexHookTrust(args, own []string) []string {
	if slices.Contains(args, codexHookTrustFlag) || slices.Contains(own, codexHookTrustFlag) {
		return args
	}
	return append(append([]string{}, args...), codexHookTrustFlag)
}

// ensureCodexHooks writes atrium's codex hooks into codexHome ("" for codex's own) where one is missing, and reports
// whether they are all there. A hook naming another room's atrium binary counts as there: two rooms on one machine
// share ~/.codex, and either binary reports to the room its card's ATRIUM_LOCATION names.
func ensureCodexHooks(codexHome, exe string) (bool, error) {
	return ensureHooks(claudeconf.CodexAt(codexHome), exe)
}

// ensureHooks writes atrium's non-optional hooks for t where one is missing, and reports whether they are all there.
func ensureHooks(t claudeconf.Target, exe string) (bool, error) {
	rep, err := claudeconf.InspectTarget(t, exe)
	if err != nil {
		return false, err
	}
	if rep.Unreadable != "" || rep.Refused != "" {
		return false, fmt.Errorf("%s hooks at %s left alone: %s%s", t.Label, rep.Path, rep.Unreadable, rep.Refused)
	}
	var missing []string
	for _, h := range rep.Hooks {
		if !h.Installed && !h.Optional {
			missing = append(missing, h.Event)
		}
	}
	if len(missing) == 0 {
		return true, nil
	}
	if _, _, err := claudeconf.InstallOnlyTarget(t, exe, missing); err != nil {
		return false, err
	}
	return true, nil
}
