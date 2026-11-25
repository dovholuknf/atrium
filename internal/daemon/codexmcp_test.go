package daemon

import (
	"path/filepath"
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
	exe := `C:\tools\atrium.exe`
	args, _, err := runnerArgsWith(codex, "", "go", launchOptions{ControlMCP: exe})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--no-alt-screen",
		"-c", "mcp_servers.atrium-control.command='" + filepath.ToSlash(exe) + "'",
		"-c", "mcp_servers.atrium-control.args=['control']",
		"-c", "mcp_servers.atrium-control.env_vars=['" + strings.Join(codexControlEnv, "','") + "']",
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
	if args, _, _ := runnerArgsWith(claude, "", "", launchOptions{ControlMCP: exe}); len(args) != 0 {
		t.Fatalf("claude was given %q", args)
	}
}
