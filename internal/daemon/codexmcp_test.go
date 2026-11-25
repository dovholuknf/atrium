package daemon

import (
	"slices"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestCodexLaunchGetsAtriumControl(t *testing.T) {
	t.Parallel()
	codex := &store.Harness{
		ID: "codex", Label: "codex", Cmd: "codex", Args: []string{"--no-alt-screen"},
		ResumeArgs: []string{"resume", "--no-alt-screen", "{resume}"}, PromptArgs: []string{"{prompt}"},
	}
	exe := "C:/tools/atrium.exe"
	args, _, err := runnerArgsWith(codex, "", "go", launchOptions{ControlMCP: exe})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--no-alt-screen",
		"-c", "mcp_servers.atrium-control.command='C:/tools/atrium.exe'",
		"-c", "mcp_servers.atrium-control.args=['control']",
		"-c", "mcp_servers.atrium-control.env_vars=['" + strings.Join(codexControlEnv, "','") + "']",
		"-c", "mcp_servers.atrium-control.default_tools_approval_mode='approve'",
		"go"}
	if !slices.Equal(args, want) {
		t.Fatalf("args %q, want %q", args, want)
	}
	if strings.ContainsRune(strings.Join(args, " "), '"') {
		t.Fatalf("a double quote would stop a codex batch shim: %q", args)
	}
	args, _, err = runnerArgsWith(codex, "sess-1", "", launchOptions{ControlMCP: exe})
	if err != nil || args[0] != "resume" || !slices.Contains(args, "mcp_servers.atrium-control.args=['control']") {
		t.Fatalf("resume args %q, %v", args, err)
	}
	own := []string{"-c", "mcp_servers.atrium-control.command='x'"}
	args, _, _ = runnerArgsWith(codex, "", "", launchOptions{ControlMCP: exe, Args: own})
	if want := []string{"--no-alt-screen", "-c", "mcp_servers.atrium-control.command='x'"}; !slices.Equal(args, want) {
		t.Fatalf("own server args %q, want %q", args, want)
	}
	claude := &store.Harness{ID: "claude", Label: "claude code", Cmd: "claude"}
	if args, _, _ := runnerArgsWith(claude, "", "", launchOptions{ControlMCP: exe, HookTrust: true}); len(args) != 0 {
		t.Fatalf("claude was given %q", args)
	}
}

func TestCodexLaunchTrustsHooksOnlyWhenAtriumWiredThem(t *testing.T) {
	t.Parallel()
	codex := &store.Harness{ID: "codex", Label: "codex", Cmd: "codex", Args: []string{"--no-alt-screen"},
		ResumeArgs: []string{"resume", "{resume}"}}
	if args, _, _ := runnerArgsWith(codex, "", "", launchOptions{}); slices.Contains(args, codexHookTrustFlag) {
		t.Fatalf("hook trust without atrium's hooks: %q", args)
	}
	args, _, _ := runnerArgsWith(codex, "sess-1", "", launchOptions{HookTrust: true})
	if want := []string{"resume", "sess-1", codexHookTrustFlag}; !slices.Equal(args, want) {
		t.Fatalf("resume args %q, want %q", args, want)
	}
	own := []string{codexHookTrustFlag}
	args, _, _ = runnerArgsWith(codex, "", "", launchOptions{HookTrust: true, Args: own})
	if want := []string{"--no-alt-screen", codexHookTrustFlag}; !slices.Equal(args, want) {
		t.Fatalf("flag given twice: %q, want %q", args, want)
	}
}

func TestCursorLaunchTrustsAndLoadsTheAtriumPlugin(t *testing.T) {
	t.Parallel()
	cursor := &store.Harness{ID: "t-cursor", Label: "cursor", Cmd: "C:/x/cursor-agent.cmd",
		ResumeArgs: []string{"--resume", "{resume}"}, PromptArgs: []string{"{prompt}"}}
	args, _, err := runnerArgsWith(cursor, "", "go", launchOptions{CursorPlugin: "D:/state/cursor-plugin"})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--trust", "--plugin-dir", "D:/state/cursor-plugin", "--approve-mcps", "go"}
	if !slices.Equal(args, want) {
		t.Fatalf("args %q, want %q", args, want)
	}
	args, _, _ = runnerArgsWith(cursor, "chat-1", "", launchOptions{Args: []string{"--trust"}})
	if want := []string{"--resume", "chat-1", "--trust"}; !slices.Equal(args, want) {
		t.Fatalf("resume args %q, want %q", args, want)
	}
}

// A gated cursor launch turns cursor's own prompt off, since cursor takes a hook's deny and never its allow. Without
// the plugin there is no gate, so there is no --force, and a row that already says --yolo is not told twice.
func TestGatedCursorLaunchLetsTheBoardApprove(t *testing.T) {
	t.Parallel()
	cursor := &store.Harness{ID: "t-cursor", Label: "cursor", Cmd: "C:/x/cursor-agent.cmd", PromptArgs: []string{"{prompt}"}}
	args, _, _ := runnerArgsWith(cursor, "", "go", launchOptions{CursorPlugin: "D:/p", CursorForce: true})
	if want := []string{"--trust", "--force", "--plugin-dir", "D:/p", "--approve-mcps", "go"}; !slices.Equal(args, want) {
		t.Fatalf("args %q, want %q", args, want)
	}
	args, _, _ = runnerArgsWith(cursor, "", "go", launchOptions{CursorForce: true})
	if slices.Contains(args, "--force") {
		t.Fatalf("no plugin, so no gate, and still --force: %q", args)
	}
	args, _, _ = runnerArgsWith(cursor, "", "go", launchOptions{CursorPlugin: "D:/p", CursorForce: true, Args: []string{"--yolo"}})
	if slices.Contains(args, "--force") {
		t.Fatalf("--yolo given and --force added: %q", args)
	}
	for env, want := range map[string]bool{"": true, "on": true, "force": true, "off": false, "OFF": false, "no": false} {
		m := map[string]string{}
		if env != "" {
			m["atrium_perm_gate"] = env
		}
		if got := gatedLaunch(m); got != want {
			t.Errorf("ATRIUM_PERM_GATE=%q: gated %v, want %v", env, got, want)
		}
	}
}
