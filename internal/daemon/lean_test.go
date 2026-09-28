package daemon

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

const leanTestMCP = `{"mcpServers":{
	"mercurius":{"type":"http","url":"http://127.0.0.1:8088/mcp"},
	"atrium-control":{"type":"http","url":"http://127.0.0.1:7778/_hub/mcp"}}}`

const leanTestSettings = `{
	"env":{"_ATRIUM_PERM_GATE":"on"},
	"permissions":{"allow":["Bash(ls:*)"]},
	"statusLine":{"type":"command","command":"bash status.sh"},
	"attribution":{"commit":"Co-Authored-By: someone"},
	"outputStyle":"explanatory",
	"hooks":{
		"PreToolUse":[{"matcher":"","hooks":[
			{"type":"command","command":"pwsh -File perm-hook.ps1"},
			{"type":"command","command":"C:/a/atrium.exe hook --event tool-start"}]}],
		"SessionStart":[{"matcher":"","hooks":[
			{"type":"command","command":"pwsh -File session-bootstrap.ps1 -Phase start"},
			{"type":"command","command":"C:/a/atrium.exe session --event start","timeout":8}]}],
		"UserPromptSubmit":[{"matcher":"","hooks":[
			{"type":"command","command":"pwsh -File filler-guard.ps1"}]}]
	}}`

func leanTestRead(files map[string]string) func(string) ([]byte, error) {
	return func(p string) ([]byte, error) {
		if s, ok := files[p]; ok {
			return []byte(s), nil
		}
		return nil, errors.New("no such file")
	}
}

func flagValue(t *testing.T, args []string, flag string) string {
	t.Helper()
	for i, a := range args {
		if a == flag && i+1 < len(args) {
			return args[i+1]
		}
	}
	t.Fatalf("%s not in %q", flag, args)
	return ""
}

func count(args []string, flag string) int {
	n := 0
	for _, a := range args {
		if a == flag {
			n++
		}
	}
	return n
}

func TestLeanArgsBuildsTheLeanFlagsAndKeepsThePromptLast(t *testing.T) {
	in := []string{"--mcp-config", "C:/atrium/mcp.json", "--strict-mcp-config",
		"--model", "claude-opus-5-5", "read BRIEF.md"}
	got, err := leanArgs(in, []byte(leanTestSettings), "", nil,
		leanTestRead(map[string]string{"C:/atrium/mcp.json": leanTestMCP}))
	if err != nil {
		t.Fatal(err)
	}
	if got[len(got)-1] != "read BRIEF.md" {
		t.Fatalf("the prompt has to stay last, got %q", got)
	}
	if flagValue(t, got, "--setting-sources") != "project,local" {
		t.Fatalf("user source not dropped: %q", got)
	}
	if flagValue(t, got, "--model") != "claude-opus-5-5" {
		t.Fatalf("model lost: %q", got)
	}
	for _, f := range []string{"--mcp-config", "--strict-mcp-config", "--settings", "--setting-sources"} {
		if count(got, f) != 1 {
			t.Fatalf("%s appears %d times in %q", f, count(got, f), got)
		}
	}
	dis := flagValue(t, got, "--disallowedTools")
	for _, tool := range []string{"Agent", "Workflow", "AskUserQuestion", "EnterWorktree"} {
		if !strings.Contains(dis, tool) {
			t.Fatalf("%s not disallowed: %s", tool, dis)
		}
	}
	for _, tool := range []string{"Bash", "Read", "Edit", "ToolSearch", "Monitor"} {
		for _, d := range strings.Split(dis, ",") {
			if d == tool {
				t.Fatalf("%s is a worker tool and must stay", tool)
			}
		}
	}
	sys := flagValue(t, got, "--append-system-prompt")
	for _, rule := range []string{"atrium_report", "claude/main", "restart atrium", "build.claude/", "trailer"} {
		if !strings.Contains(sys, rule) {
			t.Fatalf("worker prompt misses %q", rule)
		}
	}
}

func TestLeanArgsKeepsOnlyAtriumControlByDefault(t *testing.T) {
	read := leanTestRead(map[string]string{"C:/atrium/mcp.json": leanTestMCP})
	in := []string{"--mcp-config", "C:/atrium/mcp.json", "--strict-mcp-config"}
	servers := func(extra []string) map[string]any {
		got, err := leanArgs(in, nil, "", extra, read)
		if err != nil {
			t.Fatal(err)
		}
		var doc struct {
			MCPServers map[string]any `json:"mcpServers"`
		}
		if err := json.Unmarshal([]byte(flagValue(t, got, "--mcp-config")), &doc); err != nil {
			t.Fatal(err)
		}
		return doc.MCPServers
	}
	if s := servers(nil); len(s) != 1 || s["atrium-control"] == nil {
		t.Fatalf("default should be atrium-control alone, got %v", s)
	}
	if s := servers([]string{"mercurius"}); len(s) != 2 || s["mercurius"] == nil {
		t.Fatalf("mercurius asked for and not kept, got %v", s)
	}
}

func TestLeanArgsRefusesAnMCPServerTheRunnerDoesNotHave(t *testing.T) {
	in := []string{"--mcp-config", "C:/atrium/mcp.json"}
	_, err := leanArgs(in, nil, "", []string{"nosuch"},
		leanTestRead(map[string]string{"C:/atrium/mcp.json": leanTestMCP}))
	if err == nil || !strings.Contains(err.Error(), "nosuch") || !strings.Contains(err.Error(), "mercurius") {
		t.Fatalf("want a refusal naming the missing and the available servers, got %v", err)
	}
}

func TestLeanSettingsKeepsGateAndHooksAndDropsWhatPrintsIntoContext(t *testing.T) {
	raw, err := leanSettings([]byte(leanTestSettings), "")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &doc); err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{"statusLine", "outputStyle"} {
		if _, ok := doc[gone]; ok {
			t.Fatalf("%s should be dropped: %s", gone, raw)
		}
	}
	for _, kept := range []string{`"_ATRIUM_PERM_GATE":"on"`, `"Bash(ls:*)"`, `"commit":""`,
		"perm-hook.ps1", "hook --event tool-start", "session --event start", `"timeout":8`} {
		if !strings.Contains(raw, kept) {
			t.Fatalf("%s missing from %s", kept, raw)
		}
	}
	for _, gone := range []string{"session-bootstrap", "filler-guard", "UserPromptSubmit", "someone"} {
		if strings.Contains(raw, gone) {
			t.Fatalf("%s should be dropped: %s", gone, raw)
		}
	}
}

func TestLeanSettingsAddsTheStopHookWhenAsked(t *testing.T) {
	raw, err := leanSettings([]byte(leanTestSettings), "C:/a/atrium.exe turn --event end")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(raw, `"Stop"`) || !strings.Contains(raw, "turn --event end") {
		t.Fatalf("stop hook missing: %s", raw)
	}
	raw, err = leanSettings(nil, "")
	if err != nil || !strings.Contains(raw, `"hooks":{}`) {
		t.Fatalf("no user settings should still build, got %s %v", raw, err)
	}
}

func TestLeanOptionsComeFromTheRequestOrTheCard(t *testing.T) {
	if lean, _ := leanOptions(LaunchRequest{}, nil); lean {
		t.Fatal("a plain launch is not lean")
	}
	lean, mcp := leanOptions(LaunchRequest{Lean: true, MCP: []string{" mercurius", "atrium-control", "mercurius"}}, nil)
	if !lean || len(mcp) != 1 || mcp[0] != "mercurius" {
		t.Fatalf("got %v %q", lean, mcp)
	}
	card := &store.Task{Tags: mergeTags([]string{"origin:agent"}, leanTags([]string{"mercurius"}))}
	lean, mcp = leanOptions(LaunchRequest{}, card)
	if !lean || len(mcp) != 1 || mcp[0] != "mercurius" {
		t.Fatalf("a reopen of a lean card should start lean with its servers, got %v %q", lean, mcp)
	}
}
