//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dovholuknf/atrium/internal/store"
)

func TestAnArtifactOutsideTheCardIsRefused(t *testing.T) {
	d := testDaemon(t)
	card, other := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(other, "x.txt"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	task := &store.Task{Worktree: filepath.ToSlash(card)}
	err := checkArtifact(d.ap, task, filepath.Join(other, "x.txt"))
	if err == nil || !strings.Contains(err.Error(), "outside") {
		t.Fatalf("an artifact outside the card: %v", err)
	}
	if err := os.WriteFile(filepath.Join(card, "y.txt"), []byte("y"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := checkArtifact(d.ap, task, "y.txt"); err != nil {
		t.Fatalf("an artifact in the card: %v", err)
	}
}

func TestAWorktreesRepoOutsideTheCardIsRefused(t *testing.T) {
	d := testDaemon(t)
	outside := t.TempDir()
	_, err := d.worktreesRepo(outside, "")
	if err == nil || !strings.Contains(err.Error(), "not a directory inside") {
		t.Fatalf("a repo outside every root: %v", err)
	}
	missing := filepath.Join(outside, "nope")
	_, merr := d.worktreesRepo(missing, "")
	if merr == nil || strings.Replace(merr.Error(), missing, outside, 1) != err.Error() {
		t.Fatalf("a missing repo answered %v, an outside one %v: they must read the same", merr, err)
	}
}
