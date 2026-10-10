package runnersetup

import (
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestForMatchesOnTheCommandLeaf(t *testing.T) {
	for cmd, want := range map[string]*Adapter{
		"gemini": Gemini, `C:\Users\x\AppData\Roaming\npm\gemini.cmd`: Gemini, "claude": Claude,
		"codex": nil, "ollama": nil,
	} {
		if got := For(&store.Harness{Cmd: cmd}); got != want {
			t.Errorf("For(%q) = %v, want %v", cmd, got, want)
		}
	}
}

func TestWorkspaceRootsSkipsDisabledAndWorktreesOff(t *testing.T) {
	got := WorkspaceRoots([]*store.Provider{
		{Root: "D:/git/github", Enabled: true, Worktrees: true, WorktreeRoot: "D:/worktrees"},
		{Root: "E:/off", Enabled: false},
		{Root: "F:/src", Enabled: true, WorktreeRoot: "F:/ignored"},
	})
	want := []string{"D:/git/github", "D:/worktrees", "F:/src"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("got %v, want %v", got, want)
	}
}
