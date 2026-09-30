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
	"ziti":{"type":"http","url":"http://127.0.0.1:9000/mcp"},
	"datawarehouse":{"type":"http","url":"http://127.0.0.1:9001/mcp"},
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
	got, err := leanArgs(in, []byte(leanTestSettings), "", nil, leanKit{}, "",
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

func TestLeanArgsKeepsAtriumControlAndMercuriusByDefault(t *testing.T) {
	read := leanTestRead(map[string]string{
		"C:/atrium/mcp.json":  leanTestMCP,
		"C:/atrium/bare.json": `{"mcpServers":{"atrium-control":{"type":"http","url":"http://x/_hub/mcp"}}}`,
	})
	servers := func(extra []string, config ...string) map[string]any {
		if len(config) == 0 {
			config = []string{"C:/atrium/mcp.json"}
		}
		in := append([]string{"--mcp-config"}, config...)
		in = append(in, "--strict-mcp-config")
		got, err := leanArgs(in, nil, "", extra, leanKit{}, "", read)
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
	if s := servers(nil); len(s) != 2 || s["atrium-control"] == nil || s["mercurius"] == nil {
		t.Fatalf("default should be atrium-control and mercurius, got %v", s)
	}
	if s := servers([]string{"ziti"}); len(s) != 3 || s["ziti"] == nil {
		t.Fatalf("ziti asked for and not kept, got %v", s)
	}
	// A default the config does not hold is left out, not refused.
	if s := servers(nil, "C:/atrium/bare.json"); len(s) != 1 || s["atrium-control"] == nil {
		t.Fatalf("a config without mercurius should give atrium-control alone, got %v", s)
	}
}

func TestLeanArgsRefusesAnMCPServerTheRunnerDoesNotHave(t *testing.T) {
	in := []string{"--mcp-config", "C:/atrium/mcp.json"}
	_, err := leanArgs(in, nil, "", []string{"nosuch"}, leanKit{}, "",
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
	if _, ok := doc["outputStyle"]; ok {
		t.Fatalf("outputStyle should be dropped: %s", raw)
	}
	// The status line stays (r-001): it costs no tokens and posts context figures.
	if got := string(doc["statusLine"]); got != `{"type":"command","command":"bash status.sh"}` {
		t.Fatalf("statusLine should be kept as given, got %q in %s", got, raw)
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
	if lean, _, _ := leanOptions(LaunchRequest{}, nil, ""); lean {
		t.Fatal("a plain launch is not lean")
	}
	on, off := true, false
	lean, mcp, _ := leanOptions(LaunchRequest{Lean: &on, MCP: []string{" ziti", "atrium-control", "mercurius", "ziti"}}, nil, "")
	if !lean || len(mcp) != 1 || mcp[0] != "ziti" {
		t.Fatalf("got %v %q", lean, mcp)
	}
	card := &store.Task{Tags: mergeTags([]string{"origin:agent"}, leanTags([]string{"ziti"}, leanKit{}))}
	lean, mcp, _ = leanOptions(LaunchRequest{}, card, "")
	if !lean || len(mcp) != 1 || mcp[0] != "ziti" {
		t.Fatalf("a reopen of a lean card should start lean with its servers, got %v %q", lean, mcp)
	}
	// Backlog-2 item 64: false wins over the card.
	if lean, mcp, _ = leanOptions(LaunchRequest{Lean: &off, MCP: []string{"ziti"}}, card, ""); lean || len(mcp) != 0 {
		t.Fatalf("lean: false on a lean card should start it with the full setup, got %v %q", lean, mcp)
	}
}

func TestWithoutLeanTagsKeepsTheRest(t *testing.T) {
	got, cut := withoutLeanTags([]string{"origin:agent", LeanTag, "atrium:mcp:ziti", "docs"})
	if !cut || strings.Join(got, ",") != "origin:agent,docs" {
		t.Fatalf("got %q %v", got, cut)
	}
	if got, cut = withoutLeanTags([]string{LeanTag}); !cut || got == nil || len(got) != 0 {
		t.Fatalf("a card whose only tag was lean should come out empty, got %q %v", got, cut)
	}
	if _, cut = withoutLeanTags([]string{"docs"}); cut {
		t.Fatal("a card that was never lean has nothing to take out")
	}
}
