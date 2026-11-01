package shared

import (
	"os"
	"path/filepath"
	"testing"
)

func TestHasDotGitRefusesAPathThatEscapes(t *testing.T) {
	d := t.TempDir()
	if err := os.Mkdir(filepath.Join(d, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if !HasDotGit(filepath.ToSlash(d)) {
		t.Error("a checkout was not seen")
	}
	if HasDotGit(filepath.ToSlash(d) + "/sub/..") {
		t.Error("an escaping path was followed")
	}
}
