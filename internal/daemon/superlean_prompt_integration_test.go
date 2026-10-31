//go:build integration

package daemon

import (
	"strings"
	"sync"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
	"github.com/dovholuknf/atrium/internal/store"
)

// claudePositionals is what Claude Code's parser leaves as the prompt, for the options a launch writes: a variadic
// option takes every bare argument after it, and `--x=<value>` takes only its own.
func claudePositionals(args []string) []string {
	one := map[string]bool{"--setting-sources": true, "--settings": true, "--append-system-prompt": true,
		"--plugin-dir": true, "--model": true, "--effort": true, "--autocompact": true}
	many := map[string]bool{"--mcp-config": true, "--disallowedTools": true, "--tools": true, "--add-dir": true}
	var out []string
	greedy := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if strings.HasPrefix(a, "-") {
			greedy = false
			if strings.Contains(a, "=") {
				continue
			}
			if one[a] {
				i++
			}
			greedy = many[a]
			continue
		}
		if !greedy {
			out = append(out, a)
		}
	}
	return out
}

// spawnedClaude is a daemon with the room's claude row as a remote room has it, no model, effort or autocompact, and
// the argv each launch spawned.
func spawnedClaude(t *testing.T) (*Daemon, func() []string) {
	t.Helper()
	d := testDaemon(t)
	if _, err := d.st.SaveHarness(store.Harness{ID: "claude", Label: "claude code", Enabled: true, Cmd: "claude",
		Args: []string{"--strict-mcp-config"}, PromptArgs: []string{"{prompt}"}, LaunchMode: store.LaunchPTY}); err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var last []string
	fake := d.hk.agentSpawn
	d.hk.agentSpawn = func(exe string, args []string) (string, []string) {
		mu.Lock()
		last = append([]string{}, args...)
		mu.Unlock()
		return fake(exe, args)
	}
	return d, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return last
	}
}

// Six superlean launches opened at an empty prompt: `--tools <list>` came last before the prompt and took it as a tool.
func TestASuperleanLaunchDeliversItsPrompt(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, spawned := spawnedClaude(t)
	on := true
	card, err := d.Launch(shared.LaunchRequest{Harness: "claude", Cwd: t.TempDir(), Prompt: "fix the bug", Superlean: &on})
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = d.StopRunner(card.ID) }()
	args := spawned()
	if got := claudePositionals(args); len(got) != 1 || !strings.Contains(got[0], "fix the bug") {
		t.Fatalf("claude would read the prompt as %q from %q", got, args)
	}
	if flagValue(t, args, "--tools") != "Read,Edit,Write,Grep,Glob,Bash,PowerShell,ToolSearch" {
		t.Fatalf("tools changed: %q", args)
	}
}

// A launch whose prompt an option would take refuses, instead of answering running for a session with no task.
func TestALaunchWhosePromptAnOptionWouldTakeSaysSo(t *testing.T) {
	if testing.Short() {
		t.Skip("slow: real ptys, processes and timers")
	}
	d, spawned := spawnedClaude(t)
	for _, extra := range [][]string{{"--add-dir", "../shared"}, {"--allowedTools", "Bash"}, {"--debug"}} {
		_, err := d.Launch(shared.LaunchRequest{Harness: "claude", Cwd: t.TempDir(), Prompt: "fix the bug", Args: extra})
		if err == nil || !strings.Contains(err.Error(), "would never reach") || !strings.Contains(err.Error(), extra[0]) {
			t.Fatalf("%q: err = %v", extra, err)
		}
		if spawned() != nil {
			t.Fatalf("%q: a runner was started: %q", extra, spawned())
		}
	}
	card, err := d.Launch(shared.LaunchRequest{Harness: "claude", Cwd: t.TempDir(), Prompt: "fix the bug",
		Args: []string{"--add-dir=../shared"}})
	if err != nil {
		t.Fatalf("the one-token form is the way to write it: %v", err)
	}
	_ = d.StopRunner(card.ID)
}

func TestPromptTakenBy(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"go"}, ""},
		{[]string{"--tools", "Read,Bash", "go"}, "--tools"},
		{[]string{"--tools=Read,Bash", "go"}, ""},
		{[]string{"--tools", "Read", "--model", "m", "go"}, ""},
		{[]string{"--strict-mcp-config", "go"}, ""},
		{[]string{"--mcp-config", "a.json", "b.json", "go"}, "--mcp-config"},
		{[]string{"--allowedTools", "Bash", "go"}, "--allowedTools"},
		{[]string{"--resume", "go"}, "--resume"},
		{[]string{"--resume", "id", "go"}, ""},
		{[]string{"--add-dir", "x", "--", "go"}, ""},
	} {
		if got := promptTakenBy(c.args); got != c.want {
			t.Errorf("%q: got %q, want %q", c.args, got, c.want)
		}
	}
}
