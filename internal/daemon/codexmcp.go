package daemon

import (
	"path/filepath"
	"strings"
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
		"-c", codexControlKey+".env_vars=['"+strings.Join(codexControlEnv, "','")+"']")
}
