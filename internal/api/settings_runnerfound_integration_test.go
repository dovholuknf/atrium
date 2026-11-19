//go:build integration

package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestRunnerFoundSeesARunnerInstalledAfterTheRoomStarted(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("PATH", t.TempDir())
	name := "atrium-late-runner"
	file := name
	if runtime.GOOS == "windows" {
		file += ".exe"
	}
	bin := filepath.Join(home, ".local", "bin")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bin, file), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got := RunnerFound(&store.Harness{ID: "late", Cmd: name}); got == "" {
		t.Fatal("a runner in ~/.local/bin, off the room's PATH, was not found")
	}
}
