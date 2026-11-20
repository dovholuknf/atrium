//go:build integration

package daemon

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/api"
)

func TestCopySessionPutsTheTranscriptWhereClaudeLooksFromTheNewFolderAndLeavesTheOriginal(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	from, to := filepath.Join(home, "cards", "orig"), filepath.Join(home, "cards", "clone.one")
	src, err := api.ProjectDirFor(from)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	body := []byte(`{"type":"user"}` + "\n")
	if err := os.WriteFile(filepath.Join(src, "6f1c2a9e.jsonl"), body, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := copySession(from, to, "6f1c2a9e"); err != nil {
		t.Fatal(err)
	}
	dst, _ := api.ProjectDirFor(to)
	if got, err := os.ReadFile(filepath.Join(dst, "6f1c2a9e.jsonl")); err != nil || string(got) != string(body) {
		t.Fatalf("copied transcript = %q, %v", got, err)
	}
	if got, err := os.ReadFile(filepath.Join(src, "6f1c2a9e.jsonl")); err != nil || string(got) != string(body) {
		t.Fatalf("the original transcript changed: %q, %v", got, err)
	}
	for _, conv := range []string{"../escape", "", "missing"} {
		if err := copySession(from, to, conv); err == nil {
			t.Errorf("copySession(%q) was not refused", conv)
		}
	}
}

func TestCarryChangesCopiesAnUntrackedNestedRepoAndNamesWhatItCouldNotCopy(t *testing.T) {
	from, to, tmp := t.TempDir(), t.TempDir(), t.TempDir()
	for _, dir := range []string{from, to} {
		gitT(t, dir, changesBase, "init", "-q")
		writeF(t, filepath.Join(dir, "a.txt"), "a\n")
		gitT(t, dir, changesBase, "add", "a.txt")
		gitT(t, dir, changesBase, "commit", "-q", "-m", "one")
	}
	nested := filepath.Join(from, ".uncommitted", "mock-walk")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	gitT(t, nested, changesBase, "init", "-q")
	writeF(t, filepath.Join(nested, "walk.html"), "walk\n")
	gitT(t, nested, changesBase, "add", "walk.html")
	gitT(t, nested, changesBase, "commit", "-q", "-m", "walk")
	writeF(t, filepath.Join(nested, "draft.txt"), "draft\n")
	writeF(t, filepath.Join(from, "loose.txt"), "loose\n")
	writeF(t, filepath.Join(to, "loose.txt"), "already here\n")

	skipped, err := carryChanges(from, to, tmp)
	if err != nil {
		t.Fatalf("carryChanges: %v", err)
	}
	if len(skipped) != 1 || skipped[0] != "loose.txt" {
		t.Errorf("skipped = %v, want [loose.txt]", skipped)
	}
	for _, rel := range []string{"walk.html", "draft.txt"} {
		if got, err := os.ReadFile(filepath.Join(to, ".uncommitted", "mock-walk", rel)); err != nil || len(got) == 0 {
			t.Errorf("%s not carried: %q, %v", rel, got, err)
		}
	}
	if log := gitT(t, filepath.Join(to, ".uncommitted", "mock-walk"), changesBase, "log", "--format=%s"); log != "walk\n" {
		t.Errorf("the nested repo's history = %q, want walk", log)
	}
}
