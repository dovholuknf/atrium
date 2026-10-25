//go:build integration

package land

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitIn(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.name=t", "-c", "user.email=t@example.com"}, args...)...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func commitFile(t *testing.T, dir, name, msg string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(name+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitIn(t, dir, "add", name)
	gitIn(t, dir, "commit", "-q", "-m", msg)
}

func TestDuplicatesRefusesALandedPatchUnderANewSHA(t *testing.T) {
	dir := t.TempDir()
	gitIn(t, dir, "init", "-q", "-b", "main")
	commitFile(t, dir, "base", "base")
	gitIn(t, dir, "checkout", "-q", "-b", "claude/main")
	commitFile(t, dir, "a", "add a")
	commitFile(t, dir, "b", "add b")

	clean, err := Duplicates(dir, "main", "claude/main")
	if err != nil || len(clean) != 0 {
		t.Fatalf("new work must pass, got %v %v", clean, err)
	}

	// The land: the same patches under new SHAs reach main.
	gitIn(t, dir, "checkout", "-q", "main")
	commitFile(t, dir, "other", "other")
	gitIn(t, dir, "cherry-pick", "claude/main~1", "claude/main")
	gitIn(t, dir, "checkout", "-q", "claude/main")
	commitFile(t, dir, "c", "add c")

	dups, err := Duplicates(dir, "main", "claude/main")
	if err != nil {
		t.Fatal(err)
	}
	if len(dups) != 2 || !strings.HasSuffix(dups[0], "add a") || !strings.HasSuffix(dups[1], "add b") {
		t.Fatalf("want the two landed commits, got %v", dups)
	}
}
