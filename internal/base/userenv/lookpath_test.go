package userenv

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func lateTool(t *testing.T, dir, name string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLookPathFindsAToolOnTheUsersCurrentPath(t *testing.T) {
	dir := t.TempDir()
	want := lateTool(t, dir, "atrium-late-runner")
	got, err := lookPath("atrium-late-runner", func() string { return dir }, "")
	if err != nil || !sameFile(got, want) {
		t.Fatalf("got %q, %v, want %q", got, err, want)
	}
}

func TestLookPathFindsAToolInTheUsersLocalBin(t *testing.T) {
	home := t.TempDir()
	want := lateTool(t, filepath.Join(home, ".local", "bin"), "atrium-late-runner")
	got, err := lookPath("atrium-late-runner", func() string { return "" }, home)
	if err != nil || !sameFile(got, want) {
		t.Fatalf("got %q, %v, want %q", got, err, want)
	}
}

func TestLookPathKeepsTheMissWhenNothingHasIt(t *testing.T) {
	if got, err := lookPath("atrium-late-runner", func() string { return t.TempDir() }, t.TempDir()); err == nil {
		t.Fatalf("found %q", got)
	}
}

func sameFile(a, b string) bool {
	x, err1 := os.Stat(a)
	y, err2 := os.Stat(b)
	return err1 == nil && err2 == nil && os.SameFile(x, y)
}
