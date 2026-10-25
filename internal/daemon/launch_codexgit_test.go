package daemon

import (
	"slices"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

// A codex card in a worktree gets the clone's git dir as a writable root, so its sandbox lets git commit and merge.
func TestCodexWorktreeAddsItsGitDir(t *testing.T) {
	t.Parallel()
	common := "/repo/clone/.git"
	codex := &store.Harness{
		ID: "codex", Label: "codex", Cmd: "codex", Args: []string{"--no-alt-screen"},
		ResumeArgs: []string{"resume", "--no-alt-screen", "{resume}"}, PromptArgs: []string{"{prompt}"},
	}
	args, _, err := runnerArgsWith(codex, "", "go", launchOptions{GitDir: common})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"--no-alt-screen", "--add-dir", common, "go"}; !slices.Equal(args, want) {
		t.Fatalf("args %q, want %q", args, want)
	}
	args, _, err = runnerArgsWith(codex, "sess-1", "", launchOptions{GitDir: common})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"resume", "--no-alt-screen", "sess-1", "--add-dir", common}; !slices.Equal(args, want) {
		t.Fatalf("resume args %q, want %q", args, want)
	}
	claude := &store.Harness{ID: "claude", Label: "claude code", Cmd: "claude"}
	if args, _, _ := runnerArgsWith(claude, "", "", launchOptions{GitDir: common}); len(args) != 0 {
		t.Fatalf("claude was given %q", args)
	}
}
