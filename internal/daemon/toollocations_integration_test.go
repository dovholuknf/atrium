//go:build integration

package daemon

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestToolsLineNamesFoundToolsOnly(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "go")
	got := toolsLine(dir)
	if !strings.Contains(got, "go="+filepath.ToSlash(filepath.Join(dir, "go"))) || strings.Contains(got, "node=") {
		t.Fatalf("toolsLine = %q", got)
	}
	if toolsLine(t.TempDir()) != "" {
		t.Fatal("a room with no tools got a line")
	}
}

func TestToolsLineLeavesOutARefusedTool(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, "git")
	fakeTool(t, dir, "gh")
	if got := toolsLine(dir, "gh"); strings.Contains(got, "gh=") || !strings.Contains(got, "git=") {
		t.Fatalf("toolsLine = %q", got)
	}
}
