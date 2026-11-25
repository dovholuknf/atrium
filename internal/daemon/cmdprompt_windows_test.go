package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/daemon/shared"
)

func TestABatchOnlyRunnerGetsItsPromptFromAFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	exe := filepath.Join(dir, "cursor-agent.cmd")
	if err := os.WriteFile(exe, []byte("@echo off\r\npowershell -File \"%~dp0x.ps1\" %*\r\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prompt := "do the thing\n\nIf you have `atrium_git_url` (a tool), call it & go"
	args, got := cmdSafePrompt(exe, dir, []string{"--force", prompt}, prompt)
	if _, _, err := shared.ViaShellIfScript(exe, args); err != nil {
		t.Fatalf("still refused: %v", err)
	}
	if want := []string{"--force", got}; !slices.Equal(args, want) || got == prompt {
		t.Fatalf("args %q, prompt %q", args, got)
	}
	if raw, _ := os.ReadFile(filepath.Join(dir, promptFileName)); string(raw) != prompt+"\n" {
		t.Fatalf("PROMPT.md = %q", raw)
	}

	plain := filepath.Join(dir, "plain.exe")
	if err := os.WriteFile(plain, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	if args, got := cmdSafePrompt(plain, t.TempDir(), []string{prompt}, prompt); got != prompt || args[0] != prompt {
		t.Fatalf("an executable's prompt was moved: %q", got)
	}
}
