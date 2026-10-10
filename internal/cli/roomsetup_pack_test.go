package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/dovholuknf/atrium/internal/roomspec"
)

func TestWalkPackSkipsSecretLookingFiles(t *testing.T) {
	base := t.TempDir()
	put := func(rel, body string) {
		p := filepath.Join(base, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	put("agents/a.md", "agent")
	put("agents/.env", "x")
	put("skills/s/SKILL.md", "skill")
	put("skills/s/.env", "SECRET=1")
	put("skills/s/deploy.pem", "key")
	put("skills/s/.ssh/config", "x")
	src := &roomspec.PackSource{Files: map[string][]byte{}}
	if err := walkPack(base, src); err != nil {
		t.Fatal(err)
	}
	if len(src.Files) != 2 || src.Files["agents/a.md"] == nil || src.Files["skills/s/SKILL.md"] == nil {
		t.Errorf("files = %v, want only agents/a.md and skills/s/SKILL.md", src.Files)
	}
	if len(src.Skipped) != 2 {
		t.Errorf("skipped = %v, want the skill's .env and .pem", src.Skipped)
	}
}
