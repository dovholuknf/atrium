//go:build integration

package home

import (
	"path/filepath"
	"testing"
)

func TestAnExplicitClaudeMainBaseIsFetchedFromTheHubOrRefused(t *testing.T) {
	t.Parallel()
	hub := filepath.Join(t.TempDir(), "hub")
	gitT(t, filepath.Dir(hub), "init", "-q", "-b", "claude/main", hub)
	gitT(t, hub, "commit", "-q", "--allow-empty", "-m", "one")
	clone := filepath.Join(t.TempDir(), "clone")
	gitT(t, filepath.Dir(clone), "clone", "-q", hub, clone)
	gitT(t, clone, "remote", "rename", "origin", "hub")
	gitT(t, hub, "commit", "-q", "--allow-empty", "-m", "two")
	tip := gitT(t, hub, "rev-parse", "HEAD")

	dd := DirsOf(filepath.Join(t.TempDir(), "home"))
	_, base, err := AddHomeWorktree(clone, dd, "", "claude/main", "a", nil)
	if err != nil {
		t.Fatalf("launch: %v", err)
	}
	if got := gitT(t, dd.Work, "rev-parse", "HEAD"); got != tip || base != "hub/claude/main" {
		t.Fatalf("worktree at %s base %q, want the hub's %s", got, base, tip)
	}

	gitT(t, clone, "remote", "set-url", "hub", filepath.Join(t.TempDir(), "gone"))
	dd2 := DirsOf(filepath.Join(t.TempDir(), "home"))
	if _, _, err := AddHomeWorktree(clone, dd2, "", "claude/main", "b", nil); err == nil {
		t.Fatal("an unreachable hub did not refuse a launch based on claude/main")
	}
}
