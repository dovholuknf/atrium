package daemon

import (
	"os"
	"path/filepath"
	"runtime"
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

func TestWithToolDirsPutsGitForWindowsAheadOfAnEarlierGit(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Git for Windows only")
	}
	root, cyg := t.TempDir(), filepath.Join(t.TempDir(), "cygwin", "bin")
	cmd, bin := filepath.Join(root, "cmd"), filepath.Join(root, "bin")
	for _, d := range []string{cmd, bin, cyg} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fakeTool(t, cmd, "git")
	fakeTool(t, bin, "bash")
	fakeTool(t, cyg, "git")
	fakeTool(t, cyg, "bash")
	sep := string(os.PathListSeparator)
	p := cyg + sep + cmd
	got := withToolDirs([]string{"PATH=" + p}, p)
	if want := "PATH=" + cmd + sep + bin + sep + p; got[0] != want {
		t.Fatalf("env = %v, want %s", got, want)
	}
}
