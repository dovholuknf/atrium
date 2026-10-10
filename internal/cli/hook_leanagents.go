package cli

import (
	"encoding/json"
	"fmt"
	"strings"
)

// `atrium hook --event lean-agents --allow a,b`: refuses an Agent call whose
// subagent_type is not one a lean launch named in lean_agents.
//
// It rides PreToolUse on Agent and Task, in the `--settings` atrium writes for a
// lean worker that has lean_agents (internal/daemon/lean.go). The Agent tool also
// offers Claude Code's built-in types, so the tool alone is not a list. A plugin
// agent is allowed as `atrium:<name>` or by its bare name, a built-in only by the
// bare name the launch listed.
//
// A HOOK MUST NEVER FAIL A SESSION. Anything unreadable prints nothing and exits 0,
// and the call runs. The permission deny rules in --disallowedTools still hold the
// built-ins then.

// leanAgentsEvent is the --event value that selects this hook.
const leanAgentsEvent = "lean-agents"

// leanAgentsVerdict is the reason to refuse a tool call, or empty to let it run.
func leanAgentsVerdict(stdin []byte, allow string) (reason string) {
	defer func() {
		if recover() != nil {
			reason = ""
		}
	}()
	var in struct {
		ToolName  string `json:"tool_name"`
		ToolInput struct {
			SubagentType string `json:"subagent_type"`
		} `json:"tool_input"`
	}
	if len(stdin) == 0 || json.Unmarshal(stdin, &in) != nil {
		return ""
	}
	if in.ToolName != "Agent" && in.ToolName != "Task" {
		return ""
	}
	var names []string
	for _, n := range strings.Split(allow, ",") {
		if n = strings.TrimSpace(n); n != "" {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return ""
	}
	// No subagent_type is the general-purpose agent.
	kind := strings.TrimSpace(in.ToolInput.SubagentType)
	if kind == "" {
		kind = "general-purpose"
	}
	var shown []string
	for _, n := range names {
		if kind == n || kind == leanAgentsPlugin+":"+n {
			return ""
		}
		shown = append(shown, n)
	}
	return fmt.Sprintf("This worker was launched to start only these agents: %s. %q is not one of them. "+
		"Use one of those as the subagent_type, or do the work yourself.", strings.Join(shown, ", "), kind)
}

// leanAgentsPlugin is the name of the session-only plugin the agents ride in. It
// matches leanPluginName in internal/daemon.
const leanAgentsPlugin = "atrium"

// leanAgentsHook is the bytes to print for a PreToolUse payload, or nil to print nothing.
func leanAgentsHook(stdin []byte, allow string) []byte {
	reason := leanAgentsVerdict(stdin, allow)
	if strings.TrimSpace(reason) == "" {
		return nil
	}
	b, err := json.Marshal(map[string]any{"hookSpecificOutput": map[string]any{
		"hookEventName":            "PreToolUse",
		"permissionDecision":       "deny",
		"permissionDecisionReason": reason,
	}})
	if err != nil {
		return nil
	}
	return b
}
