//go:build integration

package pulls

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// A turn's paths are resolved a directory at a time and answer what resolving each in full does.
func TestInsideWorktreeResolvesADirectoryOnce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.ToSlash(dir)
	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "a", "b", "x.go"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	calls := []editCall{{path: root + "/a/b/x.go"}, {path: root + "/a/b/new.go"}, {path: root + "/a/c/deep/new.go"},
		{path: root + "/top.go"}, {path: root + "/a/../a/b/x.go"}, {path: filepath.ToSlash(filepath.Dir(dir)) + "/out.go"}}
	rels, outside := insideWorktree(root, calls)
	want, wantOutside := InsideWorktreeVia(root, calls, resolveInside)
	if !slices.Equal(rels, want) || outside != wantOutside {
		t.Fatalf("by directory %v/%d, in full %v/%d", rels, outside, want, wantOutside)
	}
}
