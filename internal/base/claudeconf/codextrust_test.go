package claudeconf

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTrustCodexProjectsAddsOnlyWhatIsMissing(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CODEX_HOME", home)
	path := filepath.Join(home, "config.toml")
	a, b := filepath.Join(home, "a"), filepath.Join(home, "b")
	if err := os.WriteFile(path, []byte("[projects.'"+a+"']\ntrust_level = \"untrusted\""), 0o600); err != nil {
		t.Fatal(err)
	}
	for range 2 {
		if err := TrustCodexProjects([]string{a, b}); err != nil {
			t.Fatal(err)
		}
	}
	got, _ := os.ReadFile(path)
	want := "[projects.'" + a + "']\ntrust_level = \"untrusted\"\n[projects.'" + b + "']\ntrust_level = \"trusted\"\n"
	if string(got) != want {
		t.Fatalf("config.toml:\n%s\nwant:\n%s", got, want)
	}
}
