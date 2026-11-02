package daemon

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeTool(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

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

func TestWithToolDirsPrependsOnlyWhatTheRunnerLacks(t *testing.T) {
	roomDir, runnerDir := t.TempDir(), t.TempDir()
	fakeTool(t, roomDir, "go")
	fakeTool(t, roomDir, "node")
	fakeTool(t, runnerDir, "node")
	sep := string(os.PathListSeparator)
	got := withToolDirs([]string{"A=1", "PATH=" + runnerDir}, roomDir+sep+runnerDir)
	if got[1] != "PATH="+roomDir+sep+runnerDir {
		t.Fatalf("env = %v", got)
	}
	same := withToolDirs([]string{"PATH=" + roomDir}, roomDir)
	if same[0] != "PATH="+roomDir {
		t.Fatalf("env = %v", same)
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
