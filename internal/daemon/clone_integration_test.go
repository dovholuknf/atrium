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
