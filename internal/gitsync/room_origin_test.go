package gitsync

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// A clone path that already holds somebody else's repository is not synced into.
func TestSyncRefusesAClonePathWhoseOriginIsAnotherRepository(t *testing.T) {
	s := freshServed(t)
	sy, root := newSyncer(t)
	clone := filepath.Join(root, "github", "o", "r")
	if err := os.MkdirAll(clone, 0o755); err != nil {
		t.Fatal(err)
	}
	git(t, clone, "init", "-q")
	git(t, clone, "remote", "add", "origin", "https://github.com/someone/else.git")
	r := sy.Sync(bg, repo, false, hubFor(s))
	if r.State != StateFailed || !strings.Contains(r.Detail, "origin") || !strings.Contains(r.Detail, "left alone") {
		t.Fatalf("got %+v", r)
	}
	if out := git(t, clone, "for-each-ref", "refs/"); out != "" {
		t.Fatalf("the clone was touched:\n%s", out)
	}
	// The same repository under another spelling is fine.
	git(t, clone, "remote", "set-url", "origin", "git@github.com:o/r.git")
	if r := sy.Sync(bg, repo, false, hubFor(s)); r.State != StateOK {
		t.Fatalf("its own origin was refused: %+v", r)
	}
}
