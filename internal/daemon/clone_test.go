package daemon

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/api"
	"github.com/dovholuknf/atrium/internal/store"
)

func TestForkArgsAreTheRecipeOnTheIdAndKeepThePrompt(t *testing.T) {
	h := &store.Harness{Label: "claude", Cmd: "claude", Args: []string{"--base"},
		ResumeArgs: []string{"--resume", "{resume}", "--strict-mcp-config"},
		ForkArgs:   []string{"--resume", "{resume}", "--fork-session", "--strict-mcp-config"},
		ModelArgs:  []string{"--model", "{model}"}, PromptArgs: []string{"{prompt}"}}
	args, logged, err := forkArgs(h, "conv-1", "carry on", launchOptions{Model: "opus", Args: []string{"--verbose"}})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"--resume", "conv-1", "--fork-session", "--strict-mcp-config", "--model", "opus", "--verbose", "carry on"}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, want %q", args, want)
	}
	if strings.Contains(logged, "carry on") || !strings.Contains(logged, "--fork-session") {
		t.Fatalf("logged = %q: want the fork and not the prompt", logged)
	}
	if args, _, err = forkArgs(h, "conv-1", "", launchOptions{}); err != nil || args[len(args)-1] != "--strict-mcp-config" {
		t.Fatalf("no prompt: args = %q, err %v", args, err)
	}
}

func TestForkArgsRefuseAPromptARunnerCannotTake(t *testing.T) {
	h := &store.Harness{Label: "claude", Cmd: "claude", ForkArgs: []string{"--resume", "{resume}", "--fork-session"}}
	if _, _, err := forkArgs(h, "conv-1", "hi", launchOptions{}); err == nil {
		t.Fatal("a prompt with no prompt arguments was not refused")
	}
}

func TestForkArgsOfCodexAreItsForkSubcommand(t *testing.T) {
	h := &store.Harness{Label: "codex", Cmd: "codex", Args: []string{"--no-alt-screen"},
		ResumeArgs: []string{"resume", "--no-alt-screen", "{resume}"},
		ForkArgs:   []string{"fork", "--no-alt-screen", "{resume}"}, PromptArgs: []string{"{prompt}"}}
	args, _, err := forkArgs(h, "0199-abc", "go on", launchOptions{})
	if want := []string{"fork", "--no-alt-screen", "0199-abc", "go on"}; err != nil || !reflect.DeepEqual(args, want) {
		t.Fatalf("args = %q, err %v, want %q", args, err, want)
	}
	if _, _, err := forkArgs(&store.Harness{Label: "x", Cmd: "x"}, "c", "", launchOptions{}); err == nil {
		t.Fatal("a runner with no recipe forked")
	}
}

func TestCopySessionPutsTheTranscriptWhereClaudeLooksFromTheNewFolderAndLeavesTheOriginal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	from, to := filepath.Join(home, "cards", "orig"), filepath.Join(home, "cards", "clone.one")
	src, err := api.ProjectDirFor(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"user"}` + "\n")
	if err := os.WriteFile(filepath.Join(src, "6f1c2a9e.jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copySession(from, to, "6f1c2a9e"); err != nil {
		t.Fatal(err)
	}
	dst, _ := api.ProjectDirFor(to)
	if got, err := os.ReadFile(filepath.Join(dst, "6f1c2a9e.jsonl")); err != nil || string(got) != string(body) {
		t.Fatalf("copied transcript = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(src, "6f1c2a9e.jsonl")); err != nil || string(got) != string(body) {
		t.Fatalf("the original transcript changed: %q, %v", got, err)
	}
	for _, conv := range []string{"../escape", "", "missing"} {
		if err := copySession(from, to, conv); err == nil {
			t.Errorf("copySession(%q) was not refused", conv)
		}
	}
}
