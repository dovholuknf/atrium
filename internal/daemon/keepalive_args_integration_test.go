//go:build integration

package daemon

import (
	"strings"
	"testing"
	"time"
)

// A lean card is forked with the lean set the card was started with, and not with
// the settings, the permission modes or the directories that would make the fork
// do work.
func TestKeepaliveForkCarriesALeanCardsPromptToolsAndMCP(t *testing.T) {
	f := newKAFix(t)
	if err := f.st.SetTags(f.task.ID, []string{LeanTag, OriginAgentTag}); err != nil {
		t.Fatal(err)
	}
	if err := f.st.SetLaunchOptions(f.task.ID, "", []string{"--dangerously-skip-permissions", "--add-dir", "/x",
		"--tools", "Bash,Read"}, nil); err != nil {
		t.Fatal(err)
	}
	old := readUserSettings
	readUserSettings = func() []byte { return []byte(`{"permissions":{"allow":["Bash"]}}`) }
	t.Cleanup(func() { readUserSettings = old })
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 1 {
		t.Fatalf("forks = %d, want 1: a lean card is warmed now", f.forks())
	}
	spec := f.specs[0]
	args := strings.Join(spec.Args, "\x00")
	for _, must := range []string{"--append-system-prompt\x00" + leanSystemPrompt,
		"--disallowedTools\x00" + strings.Join(leanDisallowed, ","), "--strict-mcp-config", "--mcp-config",
		"--tools\x00Bash,Read"} {
		if !strings.Contains(args, must) {
			t.Fatalf("lean fork lacks %q:\n%q", must, spec.Args)
		}
	}
	if n := strings.Count(args, "--settings"); n != 1 {
		t.Fatalf("--settings appears %d times, want only atrium's block-all file: %q", n, spec.Args)
	}
	if n := strings.Count(args, "--setting-sources"); n != 1 || strings.Contains(args, "project,local") {
		t.Fatalf("the lean setting sources reached the fork: %q", spec.Args)
	}
	for _, mustNot := range []string{"--dangerously-skip-permissions", "--add-dir", "/x", "permissions"} {
		if strings.Contains(args, mustNot) {
			t.Fatalf("the fork carries %q: %q", mustNot, spec.Args)
		}
	}
	if !strings.Contains(strings.Join(spec.Env, "\n"), "CLAUDE_CODE_DISABLE_AUTO_MEMORY=1") {
		t.Fatalf("a lean fork keeps auto memory on:\n%v", spec.Env)
	}
}

// A lean card naming an MCP server its runner does not have is refused, not
// forked on a different tool list.
func TestKeepaliveRefusesALeanForkItCannotRebuild(t *testing.T) {
	f := newKAFix(t)
	if err := f.st.SetTags(f.task.ID, []string{LeanTag, leanMCPTagPrefix + "nosuch"}); err != nil {
		t.Fatal(err)
	}
	f.reply(f.now.Add(-56*time.Minute), replyOpt{})
	f.tick()
	if f.forks() != 0 {
		t.Fatalf("forked %d times on an MCP set the card never had", f.forks())
	}
}

func TestKeepFlags(t *testing.T) {
	got := keepFlags([]string{"--resume", "x", "--mcp-config", "a.json", "b.json", "--verbose", "--tools=Bash",
		"--settings", "{}", "--strict-mcp-config", "--model", "m", "prompt"})
	want := []string{"--mcp-config", "a.json", "b.json", "--tools=Bash", "--strict-mcp-config"}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("got %q want %q", got, want)
	}
}
