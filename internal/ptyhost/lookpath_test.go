package ptyhost

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// A bare command is found on the spawn env's PATH even when the host's own PATH does not hold it.
func TestLookPathInUsesTheSpawnEnvPath(t *testing.T) {
	dir := t.TempDir()
	name := "atrium-lookpath-probe"
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	if err := os.WriteFile(filepath.Join(dir, file), []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir()) // the host's PATH has no such command
	key := "PATH"
	if runtime.GOOS == "windows" {
		key = "Path"
	}
	got, err := lookPathIn(name, []string{key + "=" + dir})
	if err != nil {
		t.Fatalf("not found on the spawn env's PATH: %v", err)
	}
	if filepath.Dir(got) != dir {
		t.Fatalf("resolved to %q, want a file in %q", got, dir)
	}
	if _, err := lookPathIn(name, nil); err == nil {
		t.Fatal("found on a host PATH that does not hold it")
	}
}
