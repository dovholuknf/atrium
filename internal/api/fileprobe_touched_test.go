package api

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestCommandPathsNamesAbsolutePathsOnly(t *testing.T) {
	got := commandPaths(`cd /work/a && git -C "/work/b" log; ls rel/x D:/w/c`)
	want := []string{"/work/a", "/work/b", "D:/w/c"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestToolPathsIncludesBashCommand(t *testing.T) {
	line := []byte(`{"message":{"content":[{"type":"tool_use","input":{"command":"cd /work/a && ls"}}]}}`)
	if got := toolPaths(line); !reflect.DeepEqual(got, []string{"/work/a"}) {
		t.Fatalf("got %v", got)
	}
}

func TestRepoDirsOfAFolderTheCommandEnteredIncludesItself(t *testing.T) {
	repo := t.TempDir()
	os.Mkdir(filepath.Join(repo, ".git"), 0o755)
	sub := filepath.Join(repo, "slides")
	os.Mkdir(sub, 0o755)
	os.WriteFile(filepath.Join(sub, "a.md"), nil, 0o644)
	if got := heldBy("a.md", repoDirs(sub)); got != filepath.ToSlash(sub) {
		t.Fatalf("held by %q", got)
	}
}
