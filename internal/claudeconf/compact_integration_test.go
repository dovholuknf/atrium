//go:build integration

package claudeconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAutoCompactReadsTheNarrowestFileLast(t *testing.T) {
	home, proj := t.TempDir(), t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	put := func(dir, name, body string) {
		t.Helper()
		if err := os.MkdirAll(filepath.Join(dir, ".claude"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, ".claude", name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if c := AutoCompact(proj); c.WindowK != 0 || c.Off {
		t.Fatalf("no files gave %+v", c)
	}
	put(home, "settings.json", `{"autoCompactWindow":253000,"autoCompactEnabled":true}`)
	if c := AutoCompact(proj); c.WindowK != 253 || c.Off {
		t.Fatalf("home gave %+v", c)
	}
	put(proj, "settings.local.json", `{"autoCompactWindow":143000,"autoCompactEnabled":false}`)
	if c := AutoCompact(proj); c.WindowK != 143 || !c.Off {
		t.Fatalf("project gave %+v", c)
	}
	put(proj, "settings.local.json", `not json`)
	if c := AutoCompact(proj); c.WindowK != 253 || c.Off {
		t.Fatalf("a broken file must say nothing: %+v", c)
	}
}
