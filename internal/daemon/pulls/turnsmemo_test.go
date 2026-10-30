package pulls

import (
	"path/filepath"
	"testing"
)

// A reply's edited chip resolves each named path once, not once per /replies call.
func TestEditedChipRemembersEachResolvedPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	root := filepath.ToSlash(dir)
	calls := []editCall{{path: root + "/a/x.go"}, {path: root + "/a/x.go"}, {path: root + "/b.go"}, {path: filepath.ToSlash(filepath.Dir(dir)) + "/out.go"}}
	rels, outside := InsideWorktreeVia(root, calls, ResolveInsideMemo)
	if len(rels) != 2 || outside != 1 {
		t.Fatalf("rels %v outside %d", rels, outside)
	}
	wantRels, wantOutside := insideWorktree(root, calls)
	if len(wantRels) != len(rels) || wantOutside != outside {
		t.Fatalf("memo answered %v/%d, direct %v/%d", rels, outside, wantRels, wantOutside)
	}
	insideMemo.mu.Lock()
	_, kept := insideMemo.m[insideKey{root, root + "/b.go"}]
	insideMemo.mu.Unlock()
	if !kept {
		t.Fatal("the resolved path was not kept")
	}
}
