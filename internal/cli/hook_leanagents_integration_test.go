//go:build integration

package cli

import (
	"encoding/json"
	"strings"
	"testing"
)

func agentCall(tool, kind string) []byte {
	in := map[string]any{"prompt": "x"}
	if kind != "" {
		in["subagent_type"] = kind
	}
	b, _ := json.Marshal(map[string]any{"tool_name": tool, "tool_input": in})
	return b
}

func TestLeanAgentsHookAllowsOnlyTheListed(t *testing.T) {
	for _, c := range []struct {
		tool, kind, allow string
		deny              bool
	}{
		{"Agent", "atrium:codebase-steward", "codebase-steward", false},
		{"Task", "atrium:codebase-steward", "codebase-steward", false},
		{"Agent", "Explore", "codebase-steward", true},
		{"Agent", "general-purpose", "codebase-steward", true},
		{"Agent", "", "codebase-steward", true},
		{"Agent", "Explore", "codebase-steward,Explore", false},
		{"Agent", "explore", "codebase-steward,Explore", true},
		{"Agent", "", "general-purpose", false},
		{"Agent", "atrium:other", "codebase-steward", true},
		{"Bash", "Explore", "codebase-steward", false},
		{"Agent", "Explore", "", false},
	} {
		out := leanAgentsHook(agentCall(c.tool, c.kind), c.allow)
		if (out != nil) != c.deny {
			t.Errorf("%s %q allow %q: deny=%v, want %v (%s)", c.tool, c.kind, c.allow, out != nil, c.deny, out)
		}
	}
}

func TestLeanAgentsHookRefusalNamesTheAllowedAgents(t *testing.T) {
	out := string(leanAgentsHook(agentCall("Agent", "Explore"), "codebase-steward,go-security-reviewer"))
	for _, want := range []string{`"permissionDecision":"deny"`, "codebase-steward, go-security-reviewer", `\"Explore\"`} {
		if !strings.Contains(out, want) {
			t.Errorf("refusal lacks %s: %s", want, out)
		}
	}
}

func TestLeanAgentsHookFailsOpen(t *testing.T) {
	for _, in := range []string{"", "not json", "{}", `{"tool_name":"Agent","tool_input":"x"}`} {
		if out := leanAgentsHook([]byte(in), "a"); out != nil {
			t.Errorf("%q should pass through, got %s", in, out)
		}
	}
}
